package service

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	paymentpostgres "github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
	walletdomain "github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
)

func TestWithdraw_CompletesAndCommitsAllWrites(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")

	res, err := NewWithdrawer(NewSQLDepositStore(db)).Withdraw(context.Background(), WithdrawalInput{
		UserID:   userID,
		WalletID: walletID,
		Method:   "BANK_TRANSFER",
		Amount:   decimal.NewFromInt(30),
		Currency: "USD",
	})
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if res.Payment.Status != paymentdomain.StatusCompleted {
		t.Fatalf("expected payment COMPLETED, got %s", res.Payment.Status)
	}
	if res.Transaction.Status != "COMPLETED" {
		t.Fatalf("expected transaction COMPLETED, got %s", res.Transaction.Status)
	}

	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(70)) {
		t.Fatalf("expected balance 70, got %s", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ledger_entries WHERE transaction_id = $1 AND entry_type = 'DEBIT'`, res.Transaction.ID); n != 1 {
		t.Fatalf("expected 1 DEBIT ledger entry, got %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM wallets WHERE id = $1 AND locked_balance = 0`, walletID); n != 1 {
		t.Fatal("expected locked balance to return to 0 after settlement")
	}
}

func TestWithdraw_InsufficientFundsWritesNothing(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "10")

	_, err := NewWithdrawer(NewSQLDepositStore(db)).Withdraw(context.Background(), WithdrawalInput{
		UserID:   userID,
		WalletID: walletID,
		Method:   "BANK_TRANSFER",
		Amount:   decimal.NewFromInt(30),
		Currency: "USD",
	})
	if !errors.Is(err, walletdomain.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}

	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("balance changed on rejected withdrawal: got %s, want 10", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM payments WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("payment persisted on rejected withdrawal: count %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM transactions WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("transaction persisted on rejected withdrawal: count %d", n)
	}
}

func TestWithdraw_RollsBackEverythingOnFailure(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	// Occupy the reference so the payment insert fails after the wallet row is locked.
	const taken = "WD-ROLLBACK-TEST-REF"
	existing, err := paymentdomain.NewPayment(userID, walletID, paymentdomain.DirectionWithdrawal,
		"CARD", decimal.NewFromInt(1), "USD", taken, "", nil)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	if err := paymentpostgres.New(db).Create(ctx, existing); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	_, err = NewWithdrawer(NewSQLDepositStore(db)).Withdraw(ctx, WithdrawalInput{
		UserID:    userID,
		WalletID:  walletID,
		Method:    "BANK_TRANSFER",
		Amount:    decimal.NewFromInt(30),
		Currency:  "USD",
		Reference: taken,
	})
	if err == nil {
		t.Fatal("expected duplicate reference error, got nil")
	}

	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("balance changed on failed withdrawal: got %s, want 100", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM transactions WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("transaction not rolled back: count %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ledger_entries WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("ledger entry not rolled back: count %d", n)
	}
}
