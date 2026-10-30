package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
	txdomain "github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
)

var (
	ErrEmptyProviderReference = errors.New("provider returned no collection reference")
)

// ProviderInitiator starts external payment movements: it collects deposits
// into LightEx and disburses withdrawals out. The MTN MoMo client implements
// this through Provider.Collect and Provider.Disburse; tests use fakes.
type ProviderInitiator interface {
	Collect(ctx context.Context, req provider.Request) (provider.Result, error)
	Disburse(ctx context.Context, req provider.Request) (provider.Result, error)
}

// CollectionInput describes an asynchronous deposit request. The phone number
// is required by the external collection, unlike internal-only deposits.
type CollectionInput struct {
	DepositInput
	Phone string
}

// CollectionResult is an initiated deposit now awaiting provider settlement.
// It deliberately contains no wallet or transaction change: balances move only
// when a verified callback settles the payment.
type CollectionResult struct {
	Payment           *paymentdomain.Payment
	ProviderReference string
}

// Collector initiates asynchronous external deposits. It never changes
// balances: initiation records a pending payment, the collection is started
// outside any database transaction, and the payment is moved to PROCESSING
// with its provider reference so the verified callback settlement pipeline
// (SettlementProcessor + SettleCallback) can settle it later.
type Collector struct {
	store     DepositStore
	initiator ProviderInitiator
}

// NewCollector creates an external money-movement initiator.
func NewCollector(store DepositStore, initiator ProviderInitiator) *Collector {
	return &Collector{store: store, initiator: initiator}
}

// InitiateDeposit records a PENDING deposit, starts the external collection
// and marks the payment PROCESSING with the provider reference.
//
// Write-external-write ordering never mixes the network call into a SQL
// transaction: record, collect, confirm. If collection fails, the pending
// payment is marked FAILED in its own transaction instead of dangling. A
// callback that arrives before the payment is PROCESSING is rejected by
// strict verification and will be redelivered by the provider.
//
// Reference reuse follows the established Phase 4 rules: an already-completed
// request replays without a second collection, and any other reuse, including
// a FAILED attempt, is a conflict requiring a new reference.
func (c *Collector) InitiateDeposit(ctx context.Context, in CollectionInput) (*CollectionResult, error) {
	if c == nil || c.store == nil || c.initiator == nil {
		return nil, errors.New("collector is not configured")
	}

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

	// Step 1: record PENDING only after the same wallet ownership and currency
	// checks deposits perform, so no payment row is left behind for a bad request.
	var replayed *paymentdomain.Payment
	err = c.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		if replay, err := replayExisting(ctx, r, p); err != nil {
			return err
		} else if replay != nil {
			// Already settled: idempotent retry must not call the provider again.
			replayed = replay.Payment
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
		return r.Payments.Create(ctx, p)
	})
	if err != nil {
		return nil, err
	}
	if replayed != nil {
		// Replay of an already-settled request.
		return &CollectionResult{Payment: replayed, ProviderReference: replayed.ProviderReference}, nil
	}

	// Step 2: the external call happens with no database transaction open.
	res, err := c.initiator.Collect(ctx, provider.Request{
		Reference: p.Reference,
		Amount:    p.Amount,
		Currency:  p.Currency,
		Phone:     in.Phone,
	})
	if err != nil {
		waitErr := c.failPending(ctx, p.ID)
		return nil, errors.Join(fmt.Errorf("initiate collection: %w", err), waitErr)
	}

	reference := strings.TrimSpace(res.ProviderReference)
	if reference == "" {
		waitErr := c.failPending(ctx, p.ID)
		return nil, errors.Join(ErrEmptyProviderReference, waitErr)
	}

	// Step 3: confirm PROCESSING with the provider reference. Re-locks to block
	// any concurrent writer; without a provider reference the settlement
	// pipeline cannot verify callbacks.
	var final paymentdomain.Payment
	err = c.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		locked, err := r.Payments.FindByIDForUpdate(ctx, p.ID)
		if err != nil {
			return mapErr(err)
		}
		switch locked.Status {
		case paymentdomain.StatusPending:
			if err := locked.TransitionTo(paymentdomain.StatusProcessing); err != nil {
				return err
			}
		case paymentdomain.StatusProcessing:
			// A previous attempt already confirmed processing without a reference.
		default:
			return fmt.Errorf("%w: cannot confirm initiation from status %s", paymentdomain.ErrInvalidStateTransition, locked.Status)
		}
		locked.SetProviderReference(reference)
		if err := r.Payments.Update(ctx, locked); err != nil {
			return err
		}
		final = *locked
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &CollectionResult{Payment: &final, ProviderReference: reference}, nil
}

// InitiateWithdrawal records a PENDING withdrawal, reserves its funds with
// LockFunds, creates the outbound transaction row the settlement pipeline
// reuses, starts the external disbursement and marks the payment PROCESSING
// with the provider reference.
//
// The network call never runs inside a database transaction: reserve, disburse,
// confirm. If disbursement fails, the reservation is released and the payment
// and transaction are marked FAILED in one transaction, so no funds end up
// locked for a request that will never be paid.
// A disbursement result without a provider reference is treated the same way:
// the settlement pipeline cannot verify callbacks for it.
// Reference reuse follows the established Phase 4 rules: an already-completed
// request replays without a second disbursement, and any other reuse, including
// a FAILED attempt, is a conflict requiring a new reference.
func (c *Collector) InitiateWithdrawal(ctx context.Context, in CollectionInput) (*CollectionResult, error) {
	if c == nil || c.store == nil || c.initiator == nil {
		return nil, errors.New("collector is not configured")
	}

	// Validate everything that does not need the database before any write.
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

	// Step 1: reserve funds and record the pending request in one transaction, so
	// the available-balance check cannot race and no partial state is committed.
	var replayed *paymentdomain.Payment
	err = c.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		if replay, err := replayExisting(ctx, r, p); err != nil {
			return err
		} else if replay != nil {
			// Already settled: idempotent retry must not call the provider again.
			replayed = replay.Payment
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
		// Reserve first: this is where the insufficient-funds check happens.
		if err := wallet.LockFunds(p.Amount); err != nil {
			return err
		}
		if err := r.Wallets.Update(ctx, wallet); err != nil {
			return err
		}
		if err := r.Payments.Create(ctx, p); err != nil {
			return err
		}
		txRow, err := txdomain.NewTransaction(
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
		return r.Transactions.Create(ctx, txRow)
	})
	if err != nil {
		return nil, err
	}
	if replayed != nil {
		// Replay of an already-settled request.
		return &CollectionResult{Payment: replayed, ProviderReference: replayed.ProviderReference}, nil
	}

	// Step 2: the external call happens with no database transaction open.
	res, err := c.initiator.Disburse(ctx, provider.Request{
		Reference: p.Reference,
		Amount:    p.Amount,
		Currency:  p.Currency,
		Phone:     in.Phone,
	})
	if err != nil {
		waitErr := c.failPendingWithdrawal(ctx, p.ID)
		return nil, errors.Join(fmt.Errorf("initiate withdrawal: %w", err), waitErr)
	}

	reference := strings.TrimSpace(res.ProviderReference)
	if reference == "" {
		waitErr := c.failPendingWithdrawal(ctx, p.ID)
		return nil, errors.Join(ErrEmptyProviderReference, waitErr)
	}

	// Step 3: confirm PROCESSING with the provider reference. Re-locks to block
	// any concurrent writer; without a provider reference the settlement
	// pipeline cannot verify callbacks.
	var final paymentdomain.Payment
	err = c.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		locked, err := r.Payments.FindByIDForUpdate(ctx, p.ID)
		if err != nil {
			return mapErr(err)
		}
		switch locked.Status {
		case paymentdomain.StatusPending:
			if err := locked.TransitionTo(paymentdomain.StatusProcessing); err != nil {
				return err
			}
		case paymentdomain.StatusProcessing:
			// A previous attempt already confirmed processing without a reference.
		default:
			return fmt.Errorf("%w: cannot confirm initiation from status %s", paymentdomain.ErrInvalidStateTransition, locked.Status)
		}
		locked.SetProviderReference(reference)
		if err := r.Payments.Update(ctx, locked); err != nil {
			return err
		}
		final = *locked
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &CollectionResult{Payment: &final, ProviderReference: reference}, nil
}

// failPendingWithdrawal releases a withdrawal reservation whose disbursement
// never got a provider. It must never touch a payment that already received a
// provider reference: that payment belongs to settlement or reconciliation.
func (c *Collector) failPendingWithdrawal(ctx context.Context, id uuid.UUID) error {
	return c.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		locked, err := r.Payments.FindByIDForUpdate(ctx, id)
		if err != nil {
			return mapErr(err)
		}
		if locked.Status != paymentdomain.StatusPending || locked.ProviderReference != "" {
			return nil
		}
		wallet, err := r.Wallets.FindByIDForUpdate(ctx, locked.WalletID)
		if err != nil {
			return mapWalletErr(err)
		}
		if err := wallet.UnlockFunds(locked.Amount); err != nil {
			return err
		}
		if err := r.Wallets.Update(ctx, wallet); err != nil {
			return err
		}
		if err := locked.TransitionTo(paymentdomain.StatusFailed); err != nil {
			return err
		}
		if err := r.Payments.Update(ctx, locked); err != nil {
			return err
		}
		if txRow, err := r.Transactions.FindByReference(ctx, locked.Reference); err == nil && txRow != nil {
			if err := txRow.MarkFailed("withdrawal initiation did not reach the provider"); err == nil {
				if err := r.Transactions.Update(ctx, txRow); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// failPending marks a deposit payment as FAILED. It must never touch a payment
// that already received a provider reference: a concurrent callback may have
// confirmed it, and that payment stays with reconciliation.
func (c *Collector) failPending(ctx context.Context, id uuid.UUID) error {
	return c.store.WithinDepositTx(ctx, func(r DepositRepos) error {
		locked, err := r.Payments.FindByIDForUpdate(ctx, id)
		if err != nil {
			return mapErr(err)
		}
		if locked.Status != paymentdomain.StatusPending || locked.ProviderReference != "" {
			return nil
		}
		if err := locked.TransitionTo(paymentdomain.StatusFailed); err != nil {
			return err
		}
		return r.Payments.Update(ctx, locked)
	})
}
