package reconciliation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	ledgerdomain "github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	"github.com/light-group/light-ex-backend/internal/modules/payment/service"
	txdomain "github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	walletdomain "github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
)

// In-memory mock repositories for testing reconciliation logic
type memRepos struct {
	payments     map[uuid.UUID]*paymentdomain.Payment
	transactions map[string]*txdomain.Transaction
	wallets      map[uuid.UUID]*walletdomain.Wallet
	ledger       []*ledgerdomain.Entry
}

func newMemRepos() *memRepos {
	return &memRepos{
		payments:     make(map[uuid.UUID]*paymentdomain.Payment),
		transactions: make(map[string]*txdomain.Transaction),
		wallets:      make(map[uuid.UUID]*walletdomain.Wallet),
	}
}

type memPaymentRepo struct{ mem *memRepos }

func (r *memPaymentRepo) Create(ctx context.Context, p *paymentdomain.Payment) error {
	r.mem.payments[p.ID] = p
	return nil
}
func (r *memPaymentRepo) Update(ctx context.Context, p *paymentdomain.Payment) error {
	r.mem.payments[p.ID] = p
	return nil
}
func (r *memPaymentRepo) FindByID(ctx context.Context, id uuid.UUID) (*paymentdomain.Payment, error) {
	p, ok := r.mem.payments[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}
func (r *memPaymentRepo) FindByReference(ctx context.Context, ref string) (*paymentdomain.Payment, error) {
	for _, p := range r.mem.payments {
		if p.Reference == ref {
			return p, nil
		}
	}
	return nil, errors.New("not found")
}
func (r *memPaymentRepo) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*paymentdomain.Payment, error) {
	return r.FindByID(ctx, id)
}
func (r *memPaymentRepo) FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*paymentdomain.Payment, error) {
	return nil, nil
}
func (r *memPaymentRepo) FindByStatusAndUpdatedBefore(ctx context.Context, status paymentdomain.Status, before time.Time, limit int) ([]*paymentdomain.Payment, error) {
	var res []*paymentdomain.Payment
	for _, p := range r.mem.payments {
		if p.Status == status && !p.UpdatedAt.After(before) {
			res = append(res, p)
		}
	}
	return res, nil
}

type memTxRepo struct{ mem *memRepos }

func (r *memTxRepo) Create(ctx context.Context, tx *txdomain.Transaction) error {
	r.mem.transactions[tx.Reference] = tx
	return nil
}
func (r *memTxRepo) Update(ctx context.Context, tx *txdomain.Transaction) error {
	r.mem.transactions[tx.Reference] = tx
	return nil
}
func (r *memTxRepo) FindByID(ctx context.Context, id uuid.UUID) (*txdomain.Transaction, error) {
	for _, tx := range r.mem.transactions {
		if tx.ID == id {
			return tx, nil
		}
	}
	return nil, errors.New("not found")
}
func (r *memTxRepo) FindByReference(ctx context.Context, ref string) (*txdomain.Transaction, error) {
	tx, ok := r.mem.transactions[ref]
	if !ok {
		return nil, errors.New("not found")
	}
	return tx, nil
}
func (r *memTxRepo) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*txdomain.Transaction, error) {
	return r.FindByID(ctx, id)
}
func (r *memTxRepo) FindByWalletID(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*txdomain.Transaction, error) {
	return nil, nil
}
func (r *memTxRepo) CountByWalletID(ctx context.Context, walletID uuid.UUID) (int64, error) {
	return int64(len(r.mem.transactions)), nil
}

type memWalletRepo struct{ mem *memRepos }

func (r *memWalletRepo) Create(ctx context.Context, w *walletdomain.Wallet) error {
	r.mem.wallets[w.ID] = w
	return nil
}
func (r *memWalletRepo) Update(ctx context.Context, w *walletdomain.Wallet) error {
	r.mem.wallets[w.ID] = w
	return nil
}
func (r *memWalletRepo) FindByID(ctx context.Context, id uuid.UUID) (*walletdomain.Wallet, error) {
	w, ok := r.mem.wallets[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return w, nil
}
func (r *memWalletRepo) FindByUserID(ctx context.Context, userID uuid.UUID) (*walletdomain.Wallet, error) {
	for _, w := range r.mem.wallets {
		if w.UserID == userID {
			return w, nil
		}
	}
	return nil, errors.New("not found")
}
func (r *memWalletRepo) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*walletdomain.Wallet, error) {
	return r.FindByID(ctx, id)
}
func (r *memWalletRepo) FindByUserIDForUpdate(ctx context.Context, userID uuid.UUID) (*walletdomain.Wallet, error) {
	return r.FindByUserID(ctx, userID)
}
func (r *memWalletRepo) ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	_, err := r.FindByUserID(ctx, userID)
	return err == nil, nil
}

type memLedgerRepo struct{ mem *memRepos }

func (r *memLedgerRepo) Create(ctx context.Context, e *ledgerdomain.Entry) error {
	r.mem.ledger = append(r.mem.ledger, e)
	return nil
}
func (r *memLedgerRepo) FindByID(ctx context.Context, id uuid.UUID) (*ledgerdomain.Entry, error) {
	return nil, nil
}
func (r *memLedgerRepo) FindByWalletID(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*ledgerdomain.Entry, error) {
	return nil, nil
}
func (r *memLedgerRepo) FindByTransactionID(ctx context.Context, txID uuid.UUID) ([]*ledgerdomain.Entry, error) {
	return nil, nil
}

type memStore struct{ mem *memRepos }

func (s *memStore) WithinDepositTx(ctx context.Context, fn func(r service.DepositRepos) error) error {
	repos := service.DepositRepos{
		Payments:     &memPaymentRepo{mem: s.mem},
		Transactions: &memTxRepo{mem: s.mem},
		Wallets:      &memWalletRepo{mem: s.mem},
		Ledger:       &memLedgerRepo{mem: s.mem},
	}
	return fn(repos)
}

type mockChecker struct {
	fn func(ctx context.Context, method, ref string) (ProviderStatus, error)
}

func (m *mockChecker) CheckStatus(ctx context.Context, method, ref string) (ProviderStatus, error) {
	if m.fn != nil {
		return m.fn(ctx, method, ref)
	}
	return StatusPending, nil
}

func TestReconcile_DepositSucceeded(t *testing.T) {
	mem := newMemRepos()
	userID := uuid.New()
	wallet, _ := walletdomain.NewWallet(userID, "GHS")
	mem.wallets[wallet.ID] = wallet

	payment, _ := paymentdomain.NewPayment(
		userID,
		wallet.ID,
		paymentdomain.DirectionDeposit,
		"MTN_MOMO",
		decimal.NewFromInt(100),
		"GHS",
		"DEP-REC-1",
		"Reconcile deposit",
		nil,
	)
	_ = payment.TransitionTo(paymentdomain.StatusProcessing)
	payment.UpdatedAt = time.Now().UTC().Add(-10 * time.Minute)
	mem.payments[payment.ID] = payment

	checker := &mockChecker{
		fn: func(ctx context.Context, method, ref string) (ProviderStatus, error) {
			return StatusSucceeded, nil
		},
	}

	worker := NewWorker(
		&memPaymentRepo{mem: mem},
		&memStore{mem: mem},
		checker,
		Config{StaleThreshold: 5 * time.Minute},
		nil,
	)

	count, err := worker.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 reconciled, got %d", count)
	}

	updatedPayment := mem.payments[payment.ID]
	if updatedPayment.Status != paymentdomain.StatusCompleted {
		t.Errorf("expected COMPLETED, got %v", updatedPayment.Status)
	}

	updatedWallet := mem.wallets[wallet.ID]
	if !updatedWallet.AvailableBalance.Equal(decimal.NewFromInt(100)) {
		t.Errorf("expected balance 100, got %v", updatedWallet.AvailableBalance)
	}

	if len(mem.ledger) != 1 {
		t.Errorf("expected 1 ledger entry, got %d", len(mem.ledger))
	}
}

func TestReconcile_WithdrawalFailed_UnlocksFunds(t *testing.T) {
	mem := newMemRepos()
	userID := uuid.New()
	wallet, _ := walletdomain.NewWallet(userID, "GHS")
	_ = wallet.Credit(decimal.NewFromInt(100))
	_ = wallet.LockFunds(decimal.NewFromInt(50)) // 50 available, 50 locked
	mem.wallets[wallet.ID] = wallet

	payment, _ := paymentdomain.NewPayment(
		userID,
		wallet.ID,
		paymentdomain.DirectionWithdrawal,
		"MTN_MOMO",
		decimal.NewFromInt(50),
		"GHS",
		"WTH-REC-1",
		"Withdrawal reconcile",
		nil,
	)
	_ = payment.TransitionTo(paymentdomain.StatusProcessing)
	payment.UpdatedAt = time.Now().UTC().Add(-10 * time.Minute)
	mem.payments[payment.ID] = payment

	tx, _ := txdomain.NewTransaction(wallet.ID, txdomain.TransactionTypeWithdrawal, decimal.NewFromInt(50), "GHS", "WTH-REC-1", "", nil)
	mem.transactions[tx.Reference] = tx

	checker := &mockChecker{
		fn: func(ctx context.Context, method, ref string) (ProviderStatus, error) {
			return StatusFailed, nil
		},
	}

	worker := NewWorker(
		&memPaymentRepo{mem: mem},
		&memStore{mem: mem},
		checker,
		Config{StaleThreshold: 5 * time.Minute},
		nil,
	)

	count, err := worker.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 reconciled, got %d", count)
	}

	updatedPayment := mem.payments[payment.ID]
	if updatedPayment.Status != paymentdomain.StatusFailed {
		t.Errorf("expected FAILED, got %v", updatedPayment.Status)
	}

	updatedWallet := mem.wallets[wallet.ID]
	// Available should be restored to 100, locked should be 0
	if !updatedWallet.AvailableBalance.Equal(decimal.NewFromInt(100)) {
		t.Errorf("expected available balance 100, got %v", updatedWallet.AvailableBalance)
	}
	if !updatedWallet.LockedBalance.Equal(decimal.Zero) {
		t.Errorf("expected locked balance 0, got %v", updatedWallet.LockedBalance)
	}
}
