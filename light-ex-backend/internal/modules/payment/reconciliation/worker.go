package reconciliation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	ledgerdomain "github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	paymentrepo "github.com/light-group/light-ex-backend/internal/modules/payment/repository"
	"github.com/light-group/light-ex-backend/internal/modules/payment/service"
	txdomain "github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
)

// ProviderStatus is the status reported by external payment networks.
type ProviderStatus string

const (
	StatusPending   ProviderStatus = "PENDING"
	StatusSucceeded ProviderStatus = "SUCCEEDED"
	StatusFailed    ProviderStatus = "FAILED"
)

// StatusChecker queries an external provider for the current status of a payment.
type StatusChecker interface {
	CheckStatus(ctx context.Context, method, providerReference string) (ProviderStatus, error)
}

// Config configures the background reconciliation worker.
type Config struct {
	Interval       time.Duration
	StaleThreshold time.Duration
	BatchSize      int
}

// DefaultConfig returns recommended worker settings.
func DefaultConfig() Config {
	return Config{
		Interval:       1 * time.Minute,
		StaleThreshold: 5 * time.Minute,
		BatchSize:      50,
	}
}

// Worker periodically reconciles dangling processing/pending payments with external networks.
type Worker struct {
	repo    paymentrepo.Repository
	store   service.DepositStore
	checker StatusChecker
	cfg     Config
	logger  *slog.Logger
}

// NewWorker creates a reconciliation worker.
func NewWorker(
	repo paymentrepo.Repository,
	store service.DepositStore,
	checker StatusChecker,
	cfg Config,
	logger *slog.Logger,
) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Minute
	}
	if cfg.StaleThreshold <= 0 {
		cfg.StaleThreshold = 5 * time.Minute
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}

	return &Worker{
		repo:    repo,
		store:   store,
		checker: checker,
		cfg:     cfg,
		logger:  logger,
	}
}

// Start runs the periodic reconciliation loop until the context is cancelled.
func (w *Worker) Start(ctx context.Context) error {
	w.logger.Info("Starting payment reconciliation worker", "interval", w.cfg.Interval, "stale_threshold", w.cfg.StaleThreshold)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Payment reconciliation worker stopped")
			return ctx.Err()
		case <-ticker.C:
			reconciled, err := w.ReconcileOnce(ctx)
			if err != nil {
				w.logger.Error("Reconciliation batch failed", "error", err)
			} else if reconciled > 0 {
				w.logger.Info("Reconciliation batch completed", "reconciled_count", reconciled)
			}
		}
	}
}

// ReconcileOnce performs a single reconciliation pass over stale processing payments.
func (w *Worker) ReconcileOnce(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-w.cfg.StaleThreshold)
	payments, err := w.repo.FindByStatusAndUpdatedBefore(ctx, paymentdomain.StatusProcessing, cutoff, w.cfg.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("find stale payments: %w", err)
	}

	var reconciledCount int
	for _, p := range payments {
		if err := w.reconcilePayment(ctx, p); err != nil {
			w.logger.Error("Failed to reconcile payment", "payment_id", p.ID, "reference", p.Reference, "error", err)
			continue
		}
		reconciledCount++
	}

	return reconciledCount, nil
}

func (w *Worker) reconcilePayment(ctx context.Context, p *paymentdomain.Payment) error {
	ref := p.ProviderReference
	if ref == "" {
		ref = p.Reference
	}

	status, err := w.checker.CheckStatus(ctx, p.Method, ref)
	if err != nil {
		return fmt.Errorf("check provider status: %w", err)
	}

	switch status {
	case StatusSucceeded:
		return w.settleSuccessfulPayment(ctx, p)
	case StatusFailed:
		return w.settleFailedPayment(ctx, p)
	case StatusPending:
		// External payment is still in flight, check again later
		return nil
	default:
		return fmt.Errorf("unknown provider status: %s", status)
	}
}

func (w *Worker) settleSuccessfulPayment(ctx context.Context, p *paymentdomain.Payment) error {
	return w.store.WithinDepositTx(ctx, func(r service.DepositRepos) error {
		payment, err := r.Payments.FindByIDForUpdate(ctx, p.ID)
		if err != nil {
			return err
		}
		if payment.Status != paymentdomain.StatusProcessing {
			// Already settled by another worker or webhook
			return nil
		}

		wallet, err := r.Wallets.FindByIDForUpdate(ctx, payment.WalletID)
		if err != nil {
			return err
		}

		switch payment.Direction {
		case paymentdomain.DirectionDeposit:
			// Ensure transaction exists or create completed transaction
			tx, err := r.Transactions.FindByReference(ctx, payment.Reference)
			if err != nil {
				tx, err = txdomain.NewTransaction(
					wallet.ID,
					txdomain.TransactionTypeDeposit,
					payment.Amount,
					payment.Currency,
					payment.Reference,
					payment.Description,
					map[string]any{"payment_id": payment.ID.String(), "reconciled": true},
				)
				if err != nil {
					return err
				}
				if err := r.Transactions.Create(ctx, tx); err != nil {
					return err
				}
			}

			if err := wallet.Credit(payment.Amount); err != nil {
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

		case paymentdomain.DirectionWithdrawal:
			tx, err := r.Transactions.FindByReference(ctx, payment.Reference)
			if err != nil {
				return fmt.Errorf("find withdrawal transaction: %w", err)
			}
			// Debit the locked funds that were reserved during withdrawal initiation
			if err := wallet.DebitLocked(payment.Amount); err != nil {
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
		}

		if err := payment.TransitionTo(paymentdomain.StatusCompleted); err != nil {
			return err
		}
		return r.Payments.Update(ctx, payment)
	})
}

func (w *Worker) settleFailedPayment(ctx context.Context, p *paymentdomain.Payment) error {
	return w.store.WithinDepositTx(ctx, func(r service.DepositRepos) error {
		payment, err := r.Payments.FindByIDForUpdate(ctx, p.ID)
		if err != nil {
			return err
		}
		if payment.Status != paymentdomain.StatusProcessing {
			return nil
		}

		wallet, err := r.Wallets.FindByIDForUpdate(ctx, payment.WalletID)
		if err != nil {
			return err
		}

		if payment.Direction == paymentdomain.DirectionWithdrawal {
			// Unlock funds back to available balance on withdrawal failure
			if err := wallet.UnlockFunds(payment.Amount); err != nil {
				return err
			}
			if err := r.Wallets.Update(ctx, wallet); err != nil {
				return err
			}
		}

		tx, err := r.Transactions.FindByReference(ctx, payment.Reference)
		if err == nil && tx != nil {
			if err := tx.MarkFailed("reconciliation marked failed"); err == nil {
				_ = r.Transactions.Update(ctx, tx)
			}
		}

		if err := payment.TransitionTo(paymentdomain.StatusFailed); err != nil {
			return err
		}
		return r.Payments.Update(ctx, payment)
	})
}
