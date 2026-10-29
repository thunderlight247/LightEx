package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	ledgerdomain "github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
	txdomain "github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
)

// ErrSettlementSameWallet is returned when a settlement would move funds from a wallet to itself.
var ErrSettlementSameWallet = errors.New("settlement source and destination wallets must differ")

// SettlementInput describes an internal settlement between two wallets, for example
// a user wallet paying out to the house (treasury) wallet.
type SettlementInput struct {
	FromWalletID uuid.UUID
	ToWalletID   uuid.UUID
	Amount       decimal.Decimal
	Currency     string
	Reference    string
	Description  string
}

// SettlementResult holds the two transactions created by a settlement.
type SettlementResult struct {
	From *txdomain.Transaction
	To   *txdomain.Transaction
}

// Settler moves funds between two wallets atomically.
type Settler struct {
	store DepositStore
}

// NewSettler creates a settlement orchestrator backed by the given store.
func NewSettler(store DepositStore) *Settler {
	return &Settler{store: store}
}

// Settle debits the source wallet and credits the destination wallet inside one unit of
// work. Both sides are recorded as SETTLEMENT transactions with a DEBIT and a CREDIT ledger
// entry. Any failure leaves no balance change, transaction or ledger entry behind.
func (s *Settler) Settle(ctx context.Context, in SettlementInput) (*SettlementResult, error) {
	if in.FromWalletID == uuid.Nil || in.ToWalletID == uuid.Nil {
		return nil, txdomain.ErrInvalidWalletID
	}
	if in.FromWalletID == in.ToWalletID {
		return nil, ErrSettlementSameWallet
	}

	// Validate everything that does not need the database before any write.
	ref := strings.TrimSpace(in.Reference)
	if ref == "" {
		ref = txdomain.GenerateReference("SETTLE")
	}
	fromTx, err := txdomain.NewTransaction(in.FromWalletID, txdomain.TransactionTypeSettlement,
		in.Amount, in.Currency, ref, in.Description, map[string]any{"side": "DEBIT"})
	if err != nil {
		return nil, err
	}
	toTx, err := txdomain.NewTransaction(in.ToWalletID, txdomain.TransactionTypeSettlement,
		in.Amount, in.Currency, ref+"-TO", in.Description, map[string]any{"side": "CREDIT"})
	if err != nil {
		return nil, err
	}

	var result *SettlementResult
	err = s.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		// Lock both wallets in a fixed order so concurrent settlements cannot deadlock.
		firstID, secondID := in.FromWalletID, in.ToWalletID
		if firstID.String() > secondID.String() {
			firstID, secondID = secondID, firstID
		}
		first, err := r.Wallets.FindByIDForUpdate(ctx, firstID)
		if err != nil {
			return mapWalletErr(err)
		}
		second, err := r.Wallets.FindByIDForUpdate(ctx, secondID)
		if err != nil {
			return mapWalletErr(err)
		}
		from, to := first, second
		if first.ID != in.FromWalletID {
			from, to = second, first
		}

		if from.Currency != fromTx.Currency || to.Currency != fromTx.Currency {
			return ErrDepositCurrencyMismatch
		}

		if err := r.Transactions.Create(ctx, fromTx); err != nil {
			return err
		}
		if err := r.Transactions.Create(ctx, toTx); err != nil {
			return err
		}

		if err := from.Debit(fromTx.Amount); err != nil {
			return err
		}
		if err := to.Credit(toTx.Amount); err != nil {
			return err
		}
		if err := fromTx.MarkCompleted(); err != nil {
			return err
		}
		if err := toTx.MarkCompleted(); err != nil {
			return err
		}
		if err := r.Wallets.Update(ctx, from); err != nil {
			return err
		}
		if err := r.Wallets.Update(ctx, to); err != nil {
			return err
		}
		if err := r.Transactions.Update(ctx, fromTx); err != nil {
			return err
		}
		if err := r.Transactions.Update(ctx, toTx); err != nil {
			return err
		}

		// Settlement posts both sides explicitly, so the single-side rule in
		// ledgerdomain.EntryTypeForTransaction does not apply here.
		debit, err := ledgerdomain.NewEntry(fromTx.ID, from.ID, ledgerdomain.EntryTypeDebit, fromTx.Amount, fromTx.Currency)
		if err != nil {
			return err
		}
		if err := r.Ledger.Create(ctx, debit); err != nil {
			return err
		}
		credit, err := ledgerdomain.NewEntry(toTx.ID, to.ID, ledgerdomain.EntryTypeCredit, toTx.Amount, toTx.Currency)
		if err != nil {
			return err
		}
		if err := r.Ledger.Create(ctx, credit); err != nil {
			return err
		}

		result = &SettlementResult{From: fromTx, To: toTx}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
