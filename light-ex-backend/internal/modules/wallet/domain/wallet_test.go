package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
)

func TestNewWallet(t *testing.T) {
	userID := uuid.New()

	t.Run("default currency is USD", func(t *testing.T) {
		w, err := domain.NewWallet(userID, "")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if w.UserID != userID {
			t.Errorf("expected user ID %v, got %v", userID, w.UserID)
		}
		if w.Currency != "USD" {
			t.Errorf("expected currency USD, got %v", w.Currency)
		}
		if !w.AvailableBalance.Equal(decimal.Zero) {
			t.Errorf("expected 0 available balance, got %v", w.AvailableBalance)
		}
		if !w.LockedBalance.Equal(decimal.Zero) {
			t.Errorf("expected 0 locked balance, got %v", w.LockedBalance)
		}
		if w.Status != domain.WalletStatusActive {
			t.Errorf("expected status ACTIVE, got %v", w.Status)
		}
		if w.ID == uuid.Nil {
			t.Errorf("expected non-nil wallet ID")
		}
	})

	t.Run("custom valid currency", func(t *testing.T) {
		w, err := domain.NewWallet(userID, "ghs")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if w.Currency != "GHS" {
			t.Errorf("expected currency GHS, got %v", w.Currency)
		}
	})

	t.Run("invalid user ID", func(t *testing.T) {
		_, err := domain.NewWallet(uuid.Nil, "USD")
		if err != domain.ErrInvalidUserID {
			t.Fatalf("expected ErrInvalidUserID, got: %v", err)
		}
	})

	t.Run("invalid currency length", func(t *testing.T) {
		_, err := domain.NewWallet(userID, "US")
		if err != domain.ErrInvalidCurrency {
			t.Fatalf("expected ErrInvalidCurrency, got: %v", err)
		}

		_, err = domain.NewWallet(userID, "VERYLONGCURRENCYNAME")
		if err != domain.ErrInvalidCurrency {
			t.Fatalf("expected ErrInvalidCurrency, got: %v", err)
		}
	})
}

func TestWallet_Credit(t *testing.T) {
	userID := uuid.New()
	w, err := domain.NewWallet(userID, "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("successful credit", func(t *testing.T) {
		amount := decimal.NewFromFloat(100.50)
		if err := w.Credit(amount); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !w.AvailableBalance.Equal(amount) {
			t.Errorf("expected available balance %v, got %v", amount, w.AvailableBalance)
		}
		if !w.TotalBalance().Equal(amount) {
			t.Errorf("expected total balance %v, got %v", amount, w.TotalBalance())
		}
	})

	t.Run("credit non-positive amount fails", func(t *testing.T) {
		if err := w.Credit(decimal.Zero); err != domain.ErrInvalidAmount {
			t.Errorf("expected ErrInvalidAmount, got: %v", err)
		}
		if err := w.Credit(decimal.NewFromFloat(-10.0)); err != domain.ErrInvalidAmount {
			t.Errorf("expected ErrInvalidAmount, got: %v", err)
		}
	})

	t.Run("credit inactive wallet fails", func(t *testing.T) {
		w.Suspend()
		if err := w.Credit(decimal.NewFromFloat(50)); err != domain.ErrWalletNotActive {
			t.Errorf("expected ErrWalletNotActive, got: %v", err)
		}
		w.Activate()
	})
}

func TestWallet_Debit(t *testing.T) {
	userID := uuid.New()
	w, _ := domain.NewWallet(userID, "USD")
	_ = w.Credit(decimal.NewFromFloat(100.00))

	t.Run("successful debit", func(t *testing.T) {
		debitAmt := decimal.NewFromFloat(40.00)
		if err := w.Debit(debitAmt); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		expected := decimal.NewFromFloat(60.00)
		if !w.AvailableBalance.Equal(expected) {
			t.Errorf("expected available balance %v, got %v", expected, w.AvailableBalance)
		}
	})

	t.Run("debit more than available balance fails", func(t *testing.T) {
		excessAmt := decimal.NewFromFloat(100.00)
		if err := w.Debit(excessAmt); err != domain.ErrInsufficientFunds {
			t.Errorf("expected ErrInsufficientFunds, got: %v", err)
		}
	})

	t.Run("debit non-positive amount fails", func(t *testing.T) {
		if err := w.Debit(decimal.Zero); err != domain.ErrInvalidAmount {
			t.Errorf("expected ErrInvalidAmount, got: %v", err)
		}
	})

	t.Run("debit inactive wallet fails", func(t *testing.T) {
		w.Freeze()
		if err := w.Debit(decimal.NewFromFloat(10.00)); err != domain.ErrWalletNotActive {
			t.Errorf("expected ErrWalletNotActive, got: %v", err)
		}
		w.Activate()
	})
}

func TestWallet_LockAndUnlockFunds(t *testing.T) {
	userID := uuid.New()
	w, _ := domain.NewWallet(userID, "USD")
	_ = w.Credit(decimal.NewFromFloat(200.00))

	t.Run("lock funds moves available to locked", func(t *testing.T) {
		lockAmt := decimal.NewFromFloat(50.00)
		if err := w.LockFunds(lockAmt); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !w.AvailableBalance.Equal(decimal.NewFromFloat(150.00)) {
			t.Errorf("expected 150.00 available, got %v", w.AvailableBalance)
		}
		if !w.LockedBalance.Equal(decimal.NewFromFloat(50.00)) {
			t.Errorf("expected 50.00 locked, got %v", w.LockedBalance)
		}
		if !w.TotalBalance().Equal(decimal.NewFromFloat(200.00)) {
			t.Errorf("expected 200.00 total, got %v", w.TotalBalance())
		}
	})

	t.Run("lock more than available fails", func(t *testing.T) {
		excessAmt := decimal.NewFromFloat(160.00)
		if err := w.LockFunds(excessAmt); err != domain.ErrInsufficientFunds {
			t.Errorf("expected ErrInsufficientFunds, got: %v", err)
		}
	})

	t.Run("unlock funds moves locked to available", func(t *testing.T) {
		unlockAmt := decimal.NewFromFloat(20.00)
		if err := w.UnlockFunds(unlockAmt); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !w.AvailableBalance.Equal(decimal.NewFromFloat(170.00)) {
			t.Errorf("expected 170.00 available, got %v", w.AvailableBalance)
		}
		if !w.LockedBalance.Equal(decimal.NewFromFloat(30.00)) {
			t.Errorf("expected 30.00 locked, got %v", w.LockedBalance)
		}
	})

	t.Run("unlock more than locked fails", func(t *testing.T) {
		excessAmt := decimal.NewFromFloat(40.00)
		if err := w.UnlockFunds(excessAmt); err != domain.ErrInsufficientLockedFunds {
			t.Errorf("expected ErrInsufficientLockedFunds, got: %v", err)
		}
	})

	t.Run("debit locked balance on settlement", func(t *testing.T) {
		settleAmt := decimal.NewFromFloat(30.00)
		if err := w.DebitLocked(settleAmt); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !w.LockedBalance.Equal(decimal.Zero) {
			t.Errorf("expected 0 locked balance, got %v", w.LockedBalance)
		}
		if !w.AvailableBalance.Equal(decimal.NewFromFloat(170.00)) {
			t.Errorf("expected 170.00 available, got %v", w.AvailableBalance)
		}
		if !w.TotalBalance().Equal(decimal.NewFromFloat(170.00)) {
			t.Errorf("expected 170.00 total, got %v", w.TotalBalance())
		}
	})

	t.Run("debit locked balance exceeding locked fails", func(t *testing.T) {
		if err := w.DebitLocked(decimal.NewFromFloat(1.00)); err != domain.ErrInsufficientLockedFunds {
			t.Errorf("expected ErrInsufficientLockedFunds, got: %v", err)
		}
	})
}

func TestWallet_StatusTransitions(t *testing.T) {
	userID := uuid.New()
	w, _ := domain.NewWallet(userID, "USD")

	if !w.IsActive() {
		t.Errorf("expected initially active")
	}

	w.Suspend()
	if w.Status != domain.WalletStatusSuspended || w.IsActive() {
		t.Errorf("expected suspended")
	}

	w.Freeze()
	if w.Status != domain.WalletStatusFrozen || w.IsActive() {
		t.Errorf("expected frozen")
	}

	w.Close()
	if w.Status != domain.WalletStatusClosed || w.IsActive() {
		t.Errorf("expected closed")
	}

	w.Activate()
	if w.Status != domain.WalletStatusActive || !w.IsActive() {
		t.Errorf("expected active")
	}
}
