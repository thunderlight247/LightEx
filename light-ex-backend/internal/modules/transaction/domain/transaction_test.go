package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
)

func TestNewTransaction(t *testing.T) {
	walletID := uuid.New()
	amount := decimal.NewFromFloat(100.50)

	t.Run("successful creation with default reference", func(t *testing.T) {
		tx, err := domain.NewTransaction(
			walletID,
			domain.TransactionTypeDeposit,
			amount,
			"USD",
			"",
			"Deposit funds",
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tx.WalletID != walletID {
			t.Errorf("expected wallet ID %v, got %v", walletID, tx.WalletID)
		}
		if tx.Type != domain.TransactionTypeDeposit {
			t.Errorf("expected type DEPOSIT, got %v", tx.Type)
		}
		if !tx.Amount.Equal(amount) {
			t.Errorf("expected amount %v, got %v", amount, tx.Amount)
		}
		if tx.Currency != "USD" {
			t.Errorf("expected currency USD, got %v", tx.Currency)
		}
		if tx.Status != domain.TransactionStatusPending {
			t.Errorf("expected status PENDING, got %v", tx.Status)
		}
		if !strings.HasPrefix(tx.Reference, "DEPOSIT-") {
			t.Errorf("expected reference prefix DEPOSIT-, got %v", tx.Reference)
		}
		if tx.ID == uuid.Nil {
			t.Errorf("expected non-nil ID")
		}
		if tx.Metadata == nil {
			t.Errorf("expected metadata map to be initialized")
		}
	})

	t.Run("custom reference preserved", func(t *testing.T) {
		customRef := "REF-CUSTOM-12345"
		tx, err := domain.NewTransaction(
			walletID,
			domain.TransactionTypeWithdrawal,
			amount,
			"GHS",
			customRef,
			"Momo cashout",
			map[string]any{"channel": "mtn"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.Reference != customRef {
			t.Errorf("expected reference %s, got %s", customRef, tx.Reference)
		}
		if tx.Metadata["channel"] != "mtn" {
			t.Errorf("expected metadata channel mtn, got %v", tx.Metadata["channel"])
		}
	})

	t.Run("nil wallet ID fails", func(t *testing.T) {
		_, err := domain.NewTransaction(
			uuid.Nil,
			domain.TransactionTypeDeposit,
			amount,
			"USD",
			"",
			"desc",
			nil,
		)
		if !errors.Is(err, domain.ErrInvalidWalletID) {
			t.Fatalf("expected ErrInvalidWalletID, got %v", err)
		}
	})

	t.Run("invalid transaction type fails", func(t *testing.T) {
		_, err := domain.NewTransaction(
			walletID,
			domain.TransactionType("INVALID"),
			amount,
			"USD",
			"",
			"desc",
			nil,
		)
		if !errors.Is(err, domain.ErrInvalidTransactionType) {
			t.Fatalf("expected ErrInvalidTransactionType, got %v", err)
		}
	})

	t.Run("zero or negative amount fails", func(t *testing.T) {
		_, err := domain.NewTransaction(
			walletID,
			domain.TransactionTypeDeposit,
			decimal.Zero,
			"USD",
			"",
			"desc",
			nil,
		)
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Fatalf("expected ErrInvalidAmount for zero, got %v", err)
		}

		_, err = domain.NewTransaction(
			walletID,
			domain.TransactionTypeDeposit,
			decimal.NewFromFloat(-10.0),
			"USD",
			"",
			"desc",
			nil,
		)
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Fatalf("expected ErrInvalidAmount for negative, got %v", err)
		}
	})

	t.Run("invalid currency fails", func(t *testing.T) {
		cases := []string{"", "US", "TOOLONGCURRENCY"}
		for _, cur := range cases {
			_, err := domain.NewTransaction(
				walletID,
				domain.TransactionTypeDeposit,
				amount,
				cur,
				"",
				"desc",
				nil,
			)
			if !errors.Is(err, domain.ErrInvalidCurrency) {
				t.Fatalf("expected ErrInvalidCurrency for %q, got %v", cur, err)
			}
		}
	})

	t.Run("reference too long fails", func(t *testing.T) {
		tooLong := strings.Repeat("A", 121)
		_, err := domain.NewTransaction(
			walletID,
			domain.TransactionTypeDeposit,
			amount,
			"USD",
			tooLong,
			"desc",
			nil,
		)
		if err == nil {
			t.Fatalf("expected error for reference > 120 chars, got nil")
		}
	})
}

func TestTransactionStateTransitions(t *testing.T) {
	walletID := uuid.New()
	amount := decimal.NewFromFloat(50.0)

	newTx := func(t *testing.T) *domain.Transaction {
		tx, err := domain.NewTransaction(
			walletID,
			domain.TransactionTypeDeposit,
			amount,
			"USD",
			"",
			"test",
			nil,
		)
		if err != nil {
			t.Fatalf("failed to create fixture transaction: %v", err)
		}
		return tx
	}

	t.Run("pending to processing then completed", func(t *testing.T) {
		tx := newTx(t)
		if tx.IsTerminal() {
			t.Errorf("expected pending tx to not be terminal")
		}

		if err := tx.MarkProcessing(); err != nil {
			t.Fatalf("unexpected error marking processing: %v", err)
		}
		if tx.Status != domain.TransactionStatusProcessing {
			t.Errorf("expected status PROCESSING, got %s", tx.Status)
		}
		if tx.IsTerminal() {
			t.Errorf("expected processing tx to not be terminal")
		}

		if err := tx.MarkCompleted(); err != nil {
			t.Fatalf("unexpected error marking completed: %v", err)
		}
		if tx.Status != domain.TransactionStatusCompleted {
			t.Errorf("expected status COMPLETED, got %s", tx.Status)
		}
		if !tx.IsTerminal() {
			t.Errorf("expected completed tx to be terminal")
		}
	})

	t.Run("pending directly to completed", func(t *testing.T) {
		tx := newTx(t)
		if err := tx.MarkCompleted(); err != nil {
			t.Fatalf("unexpected error completing pending tx: %v", err)
		}
		if tx.Status != domain.TransactionStatusCompleted {
			t.Errorf("expected status COMPLETED, got %s", tx.Status)
		}
	})

	t.Run("pending to failed with reason", func(t *testing.T) {
		tx := newTx(t)
		if err := tx.MarkFailed("insufficient provider liquidity"); err != nil {
			t.Fatalf("unexpected error marking failed: %v", err)
		}
		if tx.Status != domain.TransactionStatusFailed {
			t.Errorf("expected status FAILED, got %s", tx.Status)
		}
		if tx.Metadata["failure_reason"] != "insufficient provider liquidity" {
			t.Errorf("expected failure reason in metadata, got %v", tx.Metadata["failure_reason"])
		}
		if !tx.IsTerminal() {
			t.Errorf("expected failed tx to be terminal")
		}
	})

	t.Run("processing to failed with reason", func(t *testing.T) {
		tx := newTx(t)
		_ = tx.MarkProcessing()
		if err := tx.MarkFailed("network timeout"); err != nil {
			t.Fatalf("unexpected error marking failed: %v", err)
		}
		if tx.Status != domain.TransactionStatusFailed {
			t.Errorf("expected status FAILED, got %s", tx.Status)
		}
	})

	t.Run("pending to cancelled with reason", func(t *testing.T) {
		tx := newTx(t)
		if err := tx.MarkCancelled("user cancelled"); err != nil {
			t.Fatalf("unexpected error marking cancelled: %v", err)
		}
		if tx.Status != domain.TransactionStatusCancelled {
			t.Errorf("expected status CANCELLED, got %s", tx.Status)
		}
		if tx.Metadata["cancellation_reason"] != "user cancelled" {
			t.Errorf("expected cancellation reason in metadata, got %v", tx.Metadata["cancellation_reason"])
		}
		if !tx.IsTerminal() {
			t.Errorf("expected cancelled tx to be terminal")
		}
	})

	t.Run("cannot cancel processing transaction", func(t *testing.T) {
		tx := newTx(t)
		_ = tx.MarkProcessing()
		err := tx.MarkCancelled("attempted cancel")
		if !errors.Is(err, domain.ErrInvalidStateTransition) {
			t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
		}
	})

	t.Run("terminal states are immutable", func(t *testing.T) {
		tx := newTx(t)
		_ = tx.MarkCompleted()

		if err := tx.MarkProcessing(); !errors.Is(err, domain.ErrInvalidStateTransition) {
			t.Errorf("expected ErrInvalidStateTransition from completed to processing, got %v", err)
		}
		if err := tx.MarkCompleted(); !errors.Is(err, domain.ErrInvalidStateTransition) {
			t.Errorf("expected ErrInvalidStateTransition from completed to completed, got %v", err)
		}
		if err := tx.MarkFailed("error"); !errors.Is(err, domain.ErrInvalidStateTransition) {
			t.Errorf("expected ErrInvalidStateTransition from completed to failed, got %v", err)
		}
		if err := tx.MarkCancelled("error"); !errors.Is(err, domain.ErrInvalidStateTransition) {
			t.Errorf("expected ErrInvalidStateTransition from completed to cancelled, got %v", err)
		}
	})
}
