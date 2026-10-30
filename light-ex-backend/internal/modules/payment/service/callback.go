package service

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	ledgerdomain "github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
	txdomain "github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	walletdomain "github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
)

var (
	// ErrSettlementMismatch is returned when the locked payment row disagrees with
	// the verified callback facts. Nothing is written; investigation is required.
	ErrSettlementMismatch = errors.New("settlement facts disagree with the stored payment")
	// ErrUnknownSettlementStatus is returned for a provider status this settler cannot act on.
	ErrUnknownSettlementStatus = errors.New("unknown settlement status")
)

// Settlement captures the verified outcome of an external payment status that a
// callback (or reconciliation pass) asks this service to apply. It must originate
// from provider-side verification, never from untrusted webhook data alone.
type Settlement struct {
	Direction paymentdomain.Direction
	Reference string
	Amount    decimal.Decimal
	Currency  string
	Status    provider.Status
}

// SettleCallback applies a verified settlement inside the given unit of work.
// It re-locks the payment row and revalidates every fact before writing, so a
// callback racing the reconciliation worker (or another worker instance) settles
// exactly once. Idempotent: payments that are not PROCESSING are left untouched.
// The caller owns transaction safety: passing the repositories built on a SQL
// transaction whose dedup record insert is also pending guarantees that the
// callback is never recorded as handled without its effects committing together.
func SettleCallback(ctx context.Context, r DepositRepos, s Settlement) error {
	// Resolve then re-lock inside the caller's transaction to exclude concurrent writers.
	payment, err := r.Payments.FindByReference(ctx, s.Reference)
	if err != nil {
		return mapErr(err)
	}
	payment, err = r.Payments.FindByIDForUpdate(ctx, payment.ID)
	if err != nil {
		return mapErr(err)
	}

	if payment.Status != paymentdomain.StatusProcessing {
		// Already settled (or cancelled); nothing to apply.
		return nil
	}
	if payment.Direction != s.Direction || !payment.Amount.Equal(s.Amount) || payment.Currency != s.Currency {
		return ErrSettlementMismatch
	}

	wallet, err := r.Wallets.FindByIDForUpdate(ctx, payment.WalletID)
	if err != nil {
		return mapWalletErr(err)
	}

	switch s.Status {
	case provider.StatusPending:
		// Still in flight externally; wait for the next callback or reconciliation.
		return nil
	case provider.StatusSucceeded:
		return settleSuccessfulCallback(ctx, r, payment, wallet)
	case provider.StatusFailed:
		return settleFailedCallback(ctx, r, payment, wallet)
	default:
		return ErrUnknownSettlementStatus
	}
}

// settleSuccessfulCallback finalizes a processing payment the provider has confirmed.
// It mirrors the reconciliation worker's settlement so both paths keep the payment,
// transaction, wallet and ledger modules in identical states.
func settleSuccessfulCallback(ctx context.Context, r DepositRepos, payment *paymentdomain.Payment, wallet *walletdomain.Wallet) error {
	tx, err := r.Transactions.FindByReference(ctx, payment.Reference)
	if err != nil {
		if payment.Direction != paymentdomain.DirectionDeposit {
			// Withdrawal funds were locked at initiation and an outbound transaction
			// record must already exist; a missing row is an integrity problem.
			return err
		}
		// Deposits may land their callback before any transaction row exists.
		tx, err = txdomain.NewTransaction(
			wallet.ID,
			txdomain.TransactionTypeDeposit,
			payment.Amount,
			payment.Currency,
			payment.Reference,
			payment.Description,
			map[string]any{"payment_id": payment.ID.String()},
		)
		if err != nil {
			return err
		}
		if err := r.Transactions.Create(ctx, tx); err != nil {
			return err
		}
	}

	switch payment.Direction {
	case paymentdomain.DirectionDeposit:
		if err := wallet.Credit(payment.Amount); err != nil {
			return err
		}
	case paymentdomain.DirectionWithdrawal:
		// Release the reservation held at withdrawal initiation.
		if err := wallet.DebitLocked(payment.Amount); err != nil {
			return err
		}
	default:
		return paymentdomain.ErrInvalidDirection
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

	if err := payment.TransitionTo(paymentdomain.StatusCompleted); err != nil {
		return err
	}
	return r.Payments.Update(ctx, payment)
}

// settleFailedCallback records failure: withdrawal reservations are released back
// to the available balance, related transactions are marked failed, and the
// payment leaves PROCESSING. Deposits change no balances.
func settleFailedCallback(ctx context.Context, r DepositRepos, payment *paymentdomain.Payment, wallet *walletdomain.Wallet) error {
	if payment.Direction == paymentdomain.DirectionWithdrawal {
		if err := wallet.UnlockFunds(payment.Amount); err != nil {
			return err
		}
		if err := r.Wallets.Update(ctx, wallet); err != nil {
			return err
		}
	}

	tx, err := r.Transactions.FindByReference(ctx, payment.Reference)
	if err == nil && tx != nil {
		if err := tx.MarkFailed("provider reported the payment as failed"); err == nil {
			if err := r.Transactions.Update(ctx, tx); err != nil {
				return err
			}
		}
	}

	if err := payment.TransitionTo(paymentdomain.StatusFailed); err != nil {
		return err
	}
	return r.Payments.Update(ctx, payment)
}
