package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/service"
)

type mockWalletRepo struct {
	wallets       map[uuid.UUID]*domain.Wallet
	walletsByUser map[uuid.UUID]*domain.Wallet
}

func newMockWalletRepo() *mockWalletRepo {
	return &mockWalletRepo{
		wallets:       make(map[uuid.UUID]*domain.Wallet),
		walletsByUser: make(map[uuid.UUID]*domain.Wallet),
	}
}

func (m *mockWalletRepo) Create(ctx context.Context, w *domain.Wallet) error {
	m.wallets[w.ID] = w
	m.walletsByUser[w.UserID] = w
	return nil
}

func (m *mockWalletRepo) Update(ctx context.Context, w *domain.Wallet) error {
	if _, exists := m.wallets[w.ID]; !exists {
		return postgres.ErrWalletNotFound
	}
	m.wallets[w.ID] = w
	m.walletsByUser[w.UserID] = w
	return nil
}

func (m *mockWalletRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	w, exists := m.wallets[id]
	if !exists {
		return nil, postgres.ErrWalletNotFound
	}
	// return copy
	clone := *w
	return &clone, nil
}

func (m *mockWalletRepo) FindByUserID(ctx context.Context, userID uuid.UUID) (*domain.Wallet, error) {
	w, exists := m.walletsByUser[userID]
	if !exists {
		return nil, postgres.ErrWalletNotFound
	}
	clone := *w
	return &clone, nil
}

func (m *mockWalletRepo) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	return m.FindByID(ctx, id)
}

func (m *mockWalletRepo) FindByUserIDForUpdate(ctx context.Context, userID uuid.UUID) (*domain.Wallet, error) {
	return m.FindByUserID(ctx, userID)
}

func (m *mockWalletRepo) ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	_, exists := m.walletsByUser[userID]
	return exists, nil
}

func TestWalletService_CreateWallet(t *testing.T) {
	repo := newMockWalletRepo()
	svc := service.New(repo)
	ctx := context.Background()
	userID := uuid.New()

	t.Run("successfully create wallet", func(t *testing.T) {
		w, err := svc.CreateWallet(ctx, userID, "USD")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.UserID != userID {
			t.Errorf("expected userID %v, got %v", userID, w.UserID)
		}
		if w.Currency != "USD" {
			t.Errorf("expected currency USD, got %v", w.Currency)
		}
	})

	t.Run("fails when wallet already exists for user", func(t *testing.T) {
		_, err := svc.CreateWallet(ctx, userID, "USD")
		if !errors.Is(err, service.ErrWalletAlreadyExists) {
			t.Fatalf("expected ErrWalletAlreadyExists, got: %v", err)
		}
	})

	t.Run("fails with invalid user ID", func(t *testing.T) {
		_, err := svc.CreateWallet(ctx, uuid.Nil, "USD")
		if !errors.Is(err, service.ErrInvalidUserID) {
			t.Fatalf("expected ErrInvalidUserID, got: %v", err)
		}
	})

	t.Run("fails with invalid currency", func(t *testing.T) {
		newUserID := uuid.New()
		_, err := svc.CreateWallet(ctx, newUserID, "X")
		if !errors.Is(err, service.ErrInvalidCurrency) {
			t.Fatalf("expected ErrInvalidCurrency, got: %v", err)
		}
	})
}

func TestWalletService_GetWallet(t *testing.T) {
	repo := newMockWalletRepo()
	svc := service.New(repo)
	ctx := context.Background()
	userID := uuid.New()

	created, err := svc.CreateWallet(ctx, userID, "USD")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	t.Run("get by ID found", func(t *testing.T) {
		w, err := svc.GetWalletByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.ID != created.ID {
			t.Errorf("expected wallet ID %v, got %v", created.ID, w.ID)
		}
	})

	t.Run("get by ID not found", func(t *testing.T) {
		_, err := svc.GetWalletByID(ctx, uuid.New())
		if !errors.Is(err, service.ErrWalletNotFound) {
			t.Fatalf("expected ErrWalletNotFound, got: %v", err)
		}
	})

	t.Run("get by user ID found", func(t *testing.T) {
		w, err := svc.GetWalletByUserID(ctx, userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.ID != created.ID {
			t.Errorf("expected wallet ID %v, got %v", created.ID, w.ID)
		}
	})

	t.Run("get by user ID not found", func(t *testing.T) {
		_, err := svc.GetWalletByUserID(ctx, uuid.New())
		if !errors.Is(err, service.ErrWalletNotFound) {
			t.Fatalf("expected ErrWalletNotFound, got: %v", err)
		}
	})

	t.Run("get or create returns existing", func(t *testing.T) {
		w, err := svc.GetOrCreateWallet(ctx, userID, "USD")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.ID != created.ID {
			t.Errorf("expected existing wallet ID %v, got %v", created.ID, w.ID)
		}
	})

	t.Run("get or create creates new when missing", func(t *testing.T) {
		anotherUser := uuid.New()
		w, err := svc.GetOrCreateWallet(ctx, anotherUser, "EUR")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.UserID != anotherUser {
			t.Errorf("expected new wallet for user %v, got %v", anotherUser, w.UserID)
		}
	})
}

func TestWalletService_BalanceOperations(t *testing.T) {
	repo := newMockWalletRepo()
	svc := service.New(repo)
	ctx := context.Background()
	userID := uuid.New()

	w, err := svc.CreateWallet(ctx, userID, "USD")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	t.Run("credit balance", func(t *testing.T) {
		credited, err := svc.CreditBalance(ctx, w.ID, decimal.NewFromFloat(500))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !credited.AvailableBalance.Equal(decimal.NewFromFloat(500)) {
			t.Errorf("expected 500, got %v", credited.AvailableBalance)
		}
	})

	t.Run("lock balance", func(t *testing.T) {
		locked, err := svc.LockBalance(ctx, w.ID, decimal.NewFromFloat(150))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !locked.AvailableBalance.Equal(decimal.NewFromFloat(350)) {
			t.Errorf("expected 350 available, got %v", locked.AvailableBalance)
		}
		if !locked.LockedBalance.Equal(decimal.NewFromFloat(150)) {
			t.Errorf("expected 150 locked, got %v", locked.LockedBalance)
		}
	})

	t.Run("lock balance with insufficient funds", func(t *testing.T) {
		_, err := svc.LockBalance(ctx, w.ID, decimal.NewFromFloat(1000))
		if !errors.Is(err, service.ErrInsufficientFunds) {
			t.Fatalf("expected ErrInsufficientFunds, got: %v", err)
		}
	})

	t.Run("unlock balance", func(t *testing.T) {
		unlocked, err := svc.UnlockBalance(ctx, w.ID, decimal.NewFromFloat(50))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !unlocked.AvailableBalance.Equal(decimal.NewFromFloat(400)) {
			t.Errorf("expected 400 available, got %v", unlocked.AvailableBalance)
		}
		if !unlocked.LockedBalance.Equal(decimal.NewFromFloat(100)) {
			t.Errorf("expected 100 locked, got %v", unlocked.LockedBalance)
		}
	})

	t.Run("debit balance", func(t *testing.T) {
		debited, err := svc.DebitBalance(ctx, w.ID, decimal.NewFromFloat(200))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !debited.AvailableBalance.Equal(decimal.NewFromFloat(200)) {
			t.Errorf("expected 200 available, got %v", debited.AvailableBalance)
		}
	})

	t.Run("debit balance with insufficient funds", func(t *testing.T) {
		_, err := svc.DebitBalance(ctx, w.ID, decimal.NewFromFloat(500))
		if !errors.Is(err, service.ErrInsufficientFunds) {
			t.Fatalf("expected ErrInsufficientFunds, got: %v", err)
		}
	})

	t.Run("operations on non-existent wallet", func(t *testing.T) {
		randomID := uuid.New()
		_, err := svc.CreditBalance(ctx, randomID, decimal.NewFromFloat(10))
		if !errors.Is(err, service.ErrWalletNotFound) {
			t.Fatalf("expected ErrWalletNotFound, got: %v", err)
		}
	})
}
