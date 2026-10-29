package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	ledgerdomain "github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
	"github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	txpostgres "github.com/light-group/light-ex-backend/internal/modules/transaction/repository/postgres"
	walletdomain "github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
	walletpostgres "github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
)

// memStore holds in-memory state. WithinTx works on a copy and commits only on success,
// which mirrors the rollback behavior of the SQL store.
type memStore struct {
	txs     map[uuid.UUID]*domain.Transaction
	wallets map[uuid.UUID]*walletdomain.Wallet
	entries []*ledgerdomain.Entry
}

func newMemStore() *memStore {
	return &memStore{
		txs:     map[uuid.UUID]*domain.Transaction{},
		wallets: map[uuid.UUID]*walletdomain.Wallet{},
	}
}

func (m *memStore) WithinTx(ctx context.Context, fn func(r Repositories) error) error {
	snapTxs := map[uuid.UUID]domain.Transaction{}
	for id, t := range m.txs {
		snapTxs[id] = *t
	}
	snapWallets := map[uuid.UUID]walletdomain.Wallet{}
	for id, w := range m.wallets {
		snapWallets[id] = *w
	}
	snapEntries := append([]*ledgerdomain.Entry(nil), m.entries...)

	repos := Repositories{
		Transactions: &memTxRepo{store: m},
		Wallets:      &memWalletRepo{store: m},
		Ledger:       &memLedgerRepo{store: m},
	}

	if err := fn(repos); err != nil {
		m.txs = map[uuid.UUID]*domain.Transaction{}
		for id, t := range snapTxs {
			c := t
			m.txs[id] = &c
		}
		m.wallets = map[uuid.UUID]*walletdomain.Wallet{}
		for id, w := range snapWallets {
			c := w
			m.wallets[id] = &c
		}
		m.entries = snapEntries
		return err
	}
	return nil
}

type memLedgerRepo struct{ store *memStore }

func (r *memLedgerRepo) Create(_ context.Context, e *ledgerdomain.Entry) error {
	c := *e
	r.store.entries = append(r.store.entries, &c)
	return nil
}

func (r *memLedgerRepo) FindByTransactionID(_ context.Context, id uuid.UUID) ([]*ledgerdomain.Entry, error) {
	var out []*ledgerdomain.Entry
	for _, e := range r.store.entries {
		if e.TransactionID == id {
			c := *e
			out = append(out, &c)
		}
	}
	return out, nil
}

type memTxRepo struct{ store *memStore }

func (r *memTxRepo) Create(_ context.Context, tx *domain.Transaction) error {
	c := *tx
	r.store.txs[tx.ID] = &c
	return nil
}

func (r *memTxRepo) Update(_ context.Context, tx *domain.Transaction) error {
	if _, ok := r.store.txs[tx.ID]; !ok {
		return txpostgres.ErrTransactionNotFound
	}
	c := *tx
	r.store.txs[tx.ID] = &c
	return nil
}

func (r *memTxRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Transaction, error) {
	t, ok := r.store.txs[id]
	if !ok {
		return nil, txpostgres.ErrTransactionNotFound
	}
	c := *t
	return &c, nil
}

func (r *memTxRepo) FindByReference(_ context.Context, ref string) (*domain.Transaction, error) {
	for _, t := range r.store.txs {
		if t.Reference == ref {
			c := *t
			return &c, nil
		}
	}
	return nil, txpostgres.ErrTransactionNotFound
}

func (r *memTxRepo) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	return r.FindByID(ctx, id)
}

func (r *memTxRepo) FindByWalletID(_ context.Context, walletID uuid.UUID, _, _ int) ([]*domain.Transaction, error) {
	var out []*domain.Transaction
	for _, t := range r.store.txs {
		if t.WalletID == walletID {
			c := *t
			out = append(out, &c)
		}
	}
	return out, nil
}

func (r *memTxRepo) CountByWalletID(ctx context.Context, walletID uuid.UUID) (int64, error) {
	list, _ := r.FindByWalletID(ctx, walletID, 0, 0)
	return int64(len(list)), nil
}

type memWalletRepo struct{ store *memStore }

func (r *memWalletRepo) Create(_ context.Context, w *walletdomain.Wallet) error {
	c := *w
	r.store.wallets[w.ID] = &c
	return nil
}

func (r *memWalletRepo) Update(_ context.Context, w *walletdomain.Wallet) error {
	if _, ok := r.store.wallets[w.ID]; !ok {
		return walletpostgres.ErrWalletNotFound
	}
	c := *w
	r.store.wallets[w.ID] = &c
	return nil
}

func (r *memWalletRepo) FindByID(_ context.Context, id uuid.UUID) (*walletdomain.Wallet, error) {
	w, ok := r.store.wallets[id]
	if !ok {
		return nil, walletpostgres.ErrWalletNotFound
	}
	c := *w
	return &c, nil
}

func (r *memWalletRepo) FindByUserID(_ context.Context, _ uuid.UUID) (*walletdomain.Wallet, error) {
	return nil, walletpostgres.ErrWalletNotFound
}

func (r *memWalletRepo) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*walletdomain.Wallet, error) {
	return r.FindByID(ctx, id)
}

func (r *memWalletRepo) FindByUserIDForUpdate(_ context.Context, _ uuid.UUID) (*walletdomain.Wallet, error) {
	return nil, walletpostgres.ErrWalletNotFound
}

func (r *memWalletRepo) ExistsByUserID(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func setupWallet(t *testing.T, s *memStore, balance string) *walletdomain.Wallet {
	t.Helper()
	w, err := walletdomain.NewWallet(uuid.New(), "USD")
	if err != nil {
		t.Fatalf("NewWallet: %v", err)
	}
	w.AvailableBalance = decimal.RequireFromString(balance)
	s.wallets[w.ID] = w
	return w
}

func TestCreateTransaction_DoesNotTouchBalance(t *testing.T) {
	s := newMemStore()
	w := setupWallet(t, s, "100")
	svc := New(s)

	tx, err := svc.CreateTransaction(context.Background(), w.ID, domain.TransactionTypeDeposit,
		decimal.RequireFromString("50"), "USD", "", "", nil)
	if err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}
	if tx.Status != domain.TransactionStatusPending {
		t.Fatalf("expected PENDING, got %s", tx.Status)
	}
	if !s.wallets[w.ID].AvailableBalance.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("balance changed on create: %s", s.wallets[w.ID].AvailableBalance)
	}
}

func TestCreateTransaction_WalletNotFound(t *testing.T) {
	svc := New(newMemStore())
	_, err := svc.CreateTransaction(context.Background(), uuid.New(), domain.TransactionTypeDeposit,
		decimal.NewFromInt(1), "USD", "", "", nil)
	if !errors.Is(err, ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestCreateTransaction_CurrencyMismatch(t *testing.T) {
	s := newMemStore()
	w := setupWallet(t, s, "0")
	svc := New(s)

	_, err := svc.CreateTransaction(context.Background(), w.ID, domain.TransactionTypeDeposit,
		decimal.NewFromInt(1), "GHS", "", "", nil)
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestCompleteTransaction_DepositCreditsWallet(t *testing.T) {
	s := newMemStore()
	w := setupWallet(t, s, "100")
	svc := New(s)

	tx, _ := svc.CreateTransaction(context.Background(), w.ID, domain.TransactionTypeDeposit,
		decimal.RequireFromString("25.5"), "USD", "", "", nil)

	done, err := svc.CompleteTransaction(context.Background(), tx.ID)
	if err != nil {
		t.Fatalf("CompleteTransaction: %v", err)
	}
	if done.Status != domain.TransactionStatusCompleted {
		t.Fatalf("expected COMPLETED, got %s", done.Status)
	}
	if !s.wallets[w.ID].AvailableBalance.Equal(decimal.RequireFromString("125.5")) {
		t.Fatalf("expected balance 125.5, got %s", s.wallets[w.ID].AvailableBalance)
	}

	if len(s.entries) != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", len(s.entries))
	}
	e := s.entries[0]
	if e.EntryType != ledgerdomain.EntryTypeCredit || e.TransactionID != tx.ID || e.WalletID != w.ID {
		t.Fatalf("unexpected ledger entry: %+v", e)
	}
	if !e.Amount.Equal(decimal.RequireFromString("25.5")) {
		t.Fatalf("expected ledger amount 25.5, got %s", e.Amount)
	}
}

func TestCompleteTransaction_WithdrawalDebitsWallet(t *testing.T) {
	s := newMemStore()
	w := setupWallet(t, s, "100")
	svc := New(s)

	tx, _ := svc.CreateTransaction(context.Background(), w.ID, domain.TransactionTypeWithdrawal,
		decimal.NewFromInt(40), "USD", "", "", nil)

	if _, err := svc.CompleteTransaction(context.Background(), tx.ID); err != nil {
		t.Fatalf("CompleteTransaction: %v", err)
	}
	if !s.wallets[w.ID].AvailableBalance.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("expected balance 60, got %s", s.wallets[w.ID].AvailableBalance)
	}

	if len(s.entries) != 1 || s.entries[0].EntryType != ledgerdomain.EntryTypeDebit {
		t.Fatalf("expected one DEBIT ledger entry, got %+v", s.entries)
	}
}

func TestCompleteTransaction_InsufficientFundsRollsBack(t *testing.T) {
	s := newMemStore()
	w := setupWallet(t, s, "10")
	svc := New(s)

	tx, _ := svc.CreateTransaction(context.Background(), w.ID, domain.TransactionTypeWithdrawal,
		decimal.NewFromInt(50), "USD", "", "", nil)

	_, err := svc.CompleteTransaction(context.Background(), tx.ID)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}

	got, _ := svc.GetTransaction(context.Background(), tx.ID)
	if got.Status != domain.TransactionStatusPending {
		t.Fatalf("transaction should remain PENDING after rollback, got %s", got.Status)
	}
	if len(s.entries) != 0 {
		t.Fatalf("expected no ledger entries after rollback, got %d", len(s.entries))
	}
	if !s.wallets[w.ID].AvailableBalance.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("balance changed despite failure: %s", s.wallets[w.ID].AvailableBalance)
	}
}

func TestCompleteTransaction_UnsupportedTypeRejected(t *testing.T) {
	s := newMemStore()
	w := setupWallet(t, s, "100")
	svc := New(s)

	tx, _ := svc.CreateTransaction(context.Background(), w.ID, domain.TransactionTypeTransfer,
		decimal.NewFromInt(5), "USD", "", "", nil)

	_, err := svc.CompleteTransaction(context.Background(), tx.ID)
	if !errors.Is(err, ErrUnsupportedBalanceEffect) {
		t.Fatalf("expected ErrUnsupportedBalanceEffect, got %v", err)
	}
	if !s.wallets[w.ID].AvailableBalance.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("balance changed for unsupported type: %s", s.wallets[w.ID].AvailableBalance)
	}
}

func TestCompleteTransaction_CannotCompleteTwice(t *testing.T) {
	s := newMemStore()
	w := setupWallet(t, s, "0")
	svc := New(s)

	tx, _ := svc.CreateTransaction(context.Background(), w.ID, domain.TransactionTypeDeposit,
		decimal.NewFromInt(10), "USD", "", "", nil)

	if _, err := svc.CompleteTransaction(context.Background(), tx.ID); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	_, err := svc.CompleteTransaction(context.Background(), tx.ID)
	if !errors.Is(err, domain.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
	}
	if !s.wallets[w.ID].AvailableBalance.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("balance credited twice: %s", s.wallets[w.ID].AvailableBalance)
	}
}

func TestCompleteTransaction_NotFound(t *testing.T) {
	svc := New(newMemStore())
	_, err := svc.CompleteTransaction(context.Background(), uuid.New())
	if !errors.Is(err, ErrTransactionNotFound) {
		t.Fatalf("expected ErrTransactionNotFound, got %v", err)
	}
}

func TestFailTransaction_LeavesBalanceUnchanged(t *testing.T) {
	s := newMemStore()
	w := setupWallet(t, s, "100")
	svc := New(s)

	tx, _ := svc.CreateTransaction(context.Background(), w.ID, domain.TransactionTypeDeposit,
		decimal.NewFromInt(20), "USD", "", "", nil)

	failed, err := svc.FailTransaction(context.Background(), tx.ID, "provider declined")
	if err != nil {
		t.Fatalf("FailTransaction: %v", err)
	}
	if failed.Status != domain.TransactionStatusFailed {
		t.Fatalf("expected FAILED, got %s", failed.Status)
	}
	if !s.wallets[w.ID].AvailableBalance.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("balance changed on fail: %s", s.wallets[w.ID].AvailableBalance)
	}
}
