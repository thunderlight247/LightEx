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
	"github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	txrepo "github.com/light-group/light-ex-backend/internal/modules/transaction/repository"
	txpostgres "github.com/light-group/light-ex-backend/internal/modules/transaction/repository/postgres"
	walletrepo "github.com/light-group/light-ex-backend/internal/modules/wallet/repository"
	walletpostgres "github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
)

// Repositories bundles the repositories that participate in one atomic unit of work.
type Repositories struct {
	Transactions txrepo.Repository
	Wallets      walletrepo.Repository
	Ledger       ledgerrepo.Repository
}

// Store runs fn inside a single database transaction. If fn returns an error,
// every change made through the provided repositories is rolled back.
type Store interface {
	WithinTx(ctx context.Context, fn func(r Repositories) error) error
}

// Service coordinates transaction records with wallet balances.
type Service struct {
	store Store
}

// New creates a transaction service backed by the given store.
func New(store Store) *Service {
	return &Service{store: store}
}

// CreateTransaction records a new PENDING transaction for an existing wallet.
// It does not touch the wallet balance; that happens in CompleteTransaction.
func (s *Service) CreateTransaction(
	ctx context.Context,
	walletID uuid.UUID,
	txType domain.TransactionType,
	amount decimal.Decimal,
	currency string,
	reference string,
	description string,
	metadata map[string]any,
) (*domain.Transaction, error) {
	var created *domain.Transaction

	err := s.store.WithinTx(ctx, func(r Repositories) error {
		wallet, err := r.Wallets.FindByID(ctx, walletID)
		if err != nil {
			return mapWalletErr(err)
		}

		tx, err := domain.NewTransaction(wallet.ID, txType, amount, currency, reference, description, metadata)
		if err != nil {
			return err
		}
		if tx.Currency != wallet.Currency {
			return ErrCurrencyMismatch
		}

		if err := r.Transactions.Create(ctx, tx); err != nil {
			return err
		}
		created = tx
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}

// CompleteTransaction applies the transaction's balance effect and marks it COMPLETED.
// The transaction row and wallet row are locked, and both updates commit together.
// DEPOSIT credits the wallet and WITHDRAWAL debits it; other types are rejected.
func (s *Service) CompleteTransaction(ctx context.Context, txID uuid.UUID) (*domain.Transaction, error) {
	var completed *domain.Transaction

	err := s.store.WithinTx(ctx, func(r Repositories) error {
		tx, err := r.Transactions.FindByIDForUpdate(ctx, txID)
		if err != nil {
			return mapTxErr(err)
		}

		wallet, err := r.Wallets.FindByIDForUpdate(ctx, tx.WalletID)
		if err != nil {
			return mapWalletErr(err)
		}

		if tx.Currency != wallet.Currency {
			return ErrCurrencyMismatch
		}

		switch tx.Type {
		case domain.TransactionTypeDeposit:
			if err := wallet.Credit(tx.Amount); err != nil {
				return err
			}
		case domain.TransactionTypeWithdrawal:
			if err := wallet.Debit(tx.Amount); err != nil {
				return err
			}
		default:
			return ErrUnsupportedBalanceEffect
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

		completed = tx
		return nil
	})
	if err != nil {
		return nil, err
	}

	return completed, nil
}

// FailTransaction marks a PENDING or PROCESSING transaction as FAILED without
// changing the wallet balance.
func (s *Service) FailTransaction(ctx context.Context, txID uuid.UUID, reason string) (*domain.Transaction, error) {
	var failed *domain.Transaction

	err := s.store.WithinTx(ctx, func(r Repositories) error {
		tx, err := r.Transactions.FindByIDForUpdate(ctx, txID)
		if err != nil {
			return mapTxErr(err)
		}

		if err := tx.MarkFailed(reason); err != nil {
			return err
		}

		if err := r.Transactions.Update(ctx, tx); err != nil {
			return err
		}

		failed = tx
		return nil
	})
	if err != nil {
		return nil, err
	}

	return failed, nil
}

// GetTransaction returns a transaction by ID.
func (s *Service) GetTransaction(ctx context.Context, txID uuid.UUID) (*domain.Transaction, error) {
	var found *domain.Transaction

	err := s.store.WithinTx(ctx, func(r Repositories) error {
		tx, err := r.Transactions.FindByID(ctx, txID)
		if err != nil {
			return mapTxErr(err)
		}
		found = tx
		return nil
	})
	if err != nil {
		return nil, err
	}

	return found, nil
}

func mapTxErr(err error) error {
	if errors.Is(err, txpostgres.ErrTransactionNotFound) {
		return ErrTransactionNotFound
	}
	return err
}

func mapWalletErr(err error) error {
	if errors.Is(err, walletpostgres.ErrWalletNotFound) {
		return ErrWalletNotFound
	}
	return err
}

// SQLStore is the PostgreSQL implementation of Store.
type SQLStore struct {
	db *sql.DB
}

// NewSQLStore creates a Store that runs each unit of work in one SQL transaction.
func NewSQLStore(db *sql.DB) *SQLStore {
	return &SQLStore{db: db}
}

// WithinTx begins a SQL transaction, runs fn, and commits only if fn succeeds.
func (s *SQLStore) WithinTx(ctx context.Context, fn func(r Repositories) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	repos := Repositories{
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
