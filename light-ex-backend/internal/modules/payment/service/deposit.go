package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	ledgerdomain "github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
	ledgerrepo "github.com/light-group/light-ex-backend/internal/modules/ledger/repository"
	ledgerpostgres "github.com/light-group/light-ex-backend/internal/modules/ledger/repository/postgres"
	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	paymentrepo "github.com/light-group/light-ex-backend/internal/modules/payment/repository"
	paymentpostgres "github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
	txdomain "github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	txrepo "github.com/light-group/light-ex-backend/internal/modules/transaction/repository"
	txpostgres "github.com/light-group/light-ex-backend/internal/modules/transaction/repository/postgres"
	walletrepo "github.com/light-group/light-ex-backend/internal/modules/wallet/repository"
	walletpostgres "github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
)

var (
	// ErrWalletOwnerMismatch is returned when the wallet does not belong to the paying user.
	ErrWalletOwnerMismatch = errors.New("wallet does not belong to the user")
	// ErrDepositCurrencyMismatch is returned when the deposit currency differs from the wallet currency.
	ErrDepositCurrencyMismatch = errors.New("deposit currency does not match wallet currency")
)

// DepositInput describes a deposit request.
type DepositInput struct {
	UserID      uuid.UUID
	WalletID    uuid.UUID
	Method      string
	Amount      decimal.Decimal
	Currency    string
	Reference   string
	Description string
	Metadata    map[string]any
}

// DepositResult is the outcome of a completed deposit.
type DepositResult struct {
	Payment     *paymentdomain.Payment
	Transaction *txdomain.Transaction
}

// DepositRepos bundles the repositories that take part in one deposit unit of work.
type DepositRepos struct {
	Payments     paymentrepo.Repository
	Transactions txrepo.Repository
	Wallets      walletrepo.Repository
	Ledger       ledgerrepo.Repository
}

// DepositStore runs fn inside a single atomic unit of work. If fn returns an error,
// every change made through the provided repositories is rolled back.
type DepositStore interface {
	WithinDepositTx(ctx context.Context, fn func(r DepositRepos) error) error
}

// Depositor orchestrates deposits across the payment, transaction, wallet and ledger modules.
type Depositor struct {
	store DepositStore
}

// NewDepositor creates a deposit orchestrator backed by the given store.
func NewDepositor(store DepositStore) *Depositor {
	return &Depositor{store: store}
}

// Deposit validates the request, then records the payment and transaction, credits the
// wallet, posts the ledger entry and marks the payment COMPLETED. All writes commit together;
// any failure leaves no payment, transaction, balance change or ledger entry behind.
func (d *Depositor) Deposit(ctx context.Context, in DepositInput) (*DepositResult, error) {
	// Validate everything that does not need the database before any write.
	p, err := paymentdomain.NewPayment(
		in.UserID,
		in.WalletID,
		paymentdomain.DirectionDeposit,
		in.Method,
		in.Amount,
		in.Currency,
		in.Reference,
		in.Description,
		in.Metadata,
	)
	if err != nil {
		return nil, err
	}

	var result *DepositResult
	err = d.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		wallet, err := r.Wallets.FindByIDForUpdate(ctx, in.WalletID)
		if err != nil {
			return mapWalletErr(err)
		}
		if wallet.UserID != in.UserID {
			return ErrWalletOwnerMismatch
		}
		if p.Currency != wallet.Currency {
			return ErrDepositCurrencyMismatch
		}

		if err := r.Payments.Create(ctx, p); err != nil {
			return err
		}
		if err := p.TransitionTo(paymentdomain.StatusProcessing); err != nil {
			return err
		}
		if err := r.Payments.Update(ctx, p); err != nil {
			return err
		}

		tx, err := txdomain.NewTransaction(
			wallet.ID,
			txdomain.TransactionTypeDeposit,
			p.Amount,
			p.Currency,
			"",
			p.Description,
			map[string]any{"payment_id": p.ID.String()},
		)
		if err != nil {
			return err
		}
		if err := r.Transactions.Create(ctx, tx); err != nil {
			return err
		}

		if err := wallet.Credit(tx.Amount); err != nil {
			return err
		}
		if err := tx.MarkCompleted(); err != nil {
			return err
		}
		if err := r.Wallets.Update(ctx, wallet); err != nil {
			return err
		}
		if err := r.Transactions.Update(ctx, tx); err != nil {
			return err
		}

		entryType, err := ledgerdomain.EntryTypeForTransaction(string(tx.Type))
		if err != nil {
			return err
		}
		entry, err := ledgerdomain.NewEntry(tx.ID, wallet.ID, entryType, tx.Amount, tx.Currency)
		if err != nil {
			return err
		}
		if err := r.Ledger.Create(ctx, entry); err != nil {
			return err
		}

		if err := p.TransitionTo(paymentdomain.StatusCompleted); err != nil {
			return err
		}
		if err := r.Payments.Update(ctx, p); err != nil {
			return err
		}

		result = &DepositResult{Payment: p, Transaction: tx}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func mapWalletErr(err error) error {
	if errors.Is(err, walletpostgres.ErrWalletNotFound) {
		return ErrWalletNotFound
	}
	return err
}

// SQLDepositStore is the PostgreSQL implementation of DepositStore.
type SQLDepositStore struct {
	db *sql.DB
}

// NewSQLDepositStore creates a DepositStore that runs each deposit in one SQL transaction.
func NewSQLDepositStore(db *sql.DB) *SQLDepositStore {
	return &SQLDepositStore{db: db}
}

// WithinDepositTx begins a SQL transaction, runs fn, and commits only if fn succeeds.
func (s *SQLDepositStore) WithinDepositTx(ctx context.Context, fn func(r DepositRepos) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	repos := DepositRepos{
		Payments:     paymentpostgres.New(sqlTx),
		Transactions: txpostgres.New(sqlTx),
		Wallets:      walletpostgres.New(sqlTx),
		Ledger:       ledgerpostgres.New(sqlTx),
	}

	if err := fn(repos); err != nil {
		if rbErr := sqlTx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return errors.Join(err, rbErr)
		}
		return err
	}

	return sqlTx.Commit()
}
