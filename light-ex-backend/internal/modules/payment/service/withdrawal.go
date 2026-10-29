package service

import (
	"context"

	"github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	txdomain "github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
)

// WithdrawalInput describes a withdrawal request. It has the same shape as a deposit request.
type WithdrawalInput = DepositInput

// WithdrawalResult is the outcome of a completed withdrawal.
type WithdrawalResult = DepositResult

// Withdrawer orchestrates withdrawals across the payment, transaction, wallet and ledger modules.
type Withdrawer struct {
	store DepositStore
}

// NewWithdrawer creates a withdrawal orchestrator backed by the given store.
func NewWithdrawer(store DepositStore) *Withdrawer {
	return &Withdrawer{store: store}
}

// Withdraw validates the request, locks the funds, records the payment and transaction,
// settles the locked funds, posts the ledger entry and marks the payment COMPLETED.
// All writes commit together. If any step fails, including insufficient funds, nothing
// is persisted and the balance is unchanged.
func (w *Withdrawer) Withdraw(ctx context.Context, in WithdrawalInput) (*WithdrawalResult, error) {
	p, err := paymentdomain.NewPayment(
		in.UserID,
		in.WalletID,
		paymentdomain.DirectionWithdrawal,
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

	var result *WithdrawalResult
	err = w.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		if replay, err := replayExisting(ctx, r, p); err != nil {
			return err
		} else if replay != nil {
			result = replay
			return nil
		}

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
			txdomain.TransactionTypeWithdrawal,
			p.Amount,
			p.Currency,
			p.Reference,
			p.Description,
			map[string]any{"payment_id": p.ID.String()},
		)
		if err != nil {
			return err
		}
		if err := r.Transactions.Create(ctx, tx); err != nil {
			return err
		}

		// Reserve funds first; this is where the available-balance check happens.
		if err := wallet.LockFunds(tx.Amount); err != nil {
			return err
		}
		// Settle the reservation: the locked funds leave the wallet.
		if err := wallet.DebitLocked(tx.Amount); err != nil {
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

		entryType, err := domain.EntryTypeForTransaction(string(tx.Type))
		if err != nil {
			return err
		}
		entry, err := domain.NewEntry(tx.ID, wallet.ID, entryType, tx.Amount, tx.Currency)
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

		result = &WithdrawalResult{Payment: p, Transaction: tx}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
