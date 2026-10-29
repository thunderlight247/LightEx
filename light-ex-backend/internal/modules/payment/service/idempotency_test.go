package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	paymentpostgres "github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
)

func TestDeposit_RetrySameReferenceReturnsOriginalWithoutSecondCredit(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	depositor := NewDepositor(NewSQLDepositStore(db))
	in := DepositInput{
		UserID:    userID,
		WalletID:  walletID,
		Method:    "BANK_TRANSFER",
		Amount:    decimal.NewFromInt(25),
		Currency:  "USD",
		Reference: "IDEM-" + uuid.NewString(),
	}

	first, err := depositor.Deposit(context.Background(), in)
	if err != nil {
		t.Fatalf("first Deposit: %v", err)
	}
	second, err := depositor.Deposit(context.Background(), in)
	if err != nil {
		t.Fatalf("retry Deposit: %v", err)
	}

	if second.Payment.ID != first.Payment.ID || second.Transaction.ID != first.Transaction.ID {
		t.Fatalf("retry returned different records: payment %s vs %s", second.Payment.ID, first.Payment.ID)
	}
	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(125)) {
		t.Fatalf("balance after retry: got %s, want 125 (credited once)", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ledger_entries WHERE wallet_id = $1`, walletID); n != 1 {
		t.Fatalf("expected 1 ledger entry after retry, got %d", n)
	}
}

func TestDeposit_ReusedReferenceDifferentAmountIsConflict(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	depositor := NewDepositor(NewSQLDepositStore(db))
	ref := "IDEM-" + uuid.NewString()

	if _, err := depositor.Deposit(context.Background(), DepositInput{
		UserID: userID, WalletID: walletID, Method: "BANK_TRANSFER",
		Amount: decimal.NewFromInt(25), Currency: "USD", Reference: ref,
	}); err != nil {
		t.Fatalf("first Deposit: %v", err)
	}

	_, err := depositor.Deposit(context.Background(), DepositInput{
		UserID: userID, WalletID: walletID, Method: "BANK_TRANSFER",
		Amount: decimal.NewFromInt(50), Currency: "USD", Reference: ref,
	})
	if !errors.Is(err, ErrReferenceConflict) {
		t.Fatalf("expected ErrReferenceConflict, got %v", err)
	}
	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(125)) {
		t.Fatalf("balance changed on conflict: got %s, want 125", got)
	}
}

// A FAILED payment is final: its reference cannot be reused, and the user
// must start a new request with a new reference.
func TestDeposit_FailedPaymentReferenceCannotBeReused(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()
	ref := "IDEM-FAILED-" + uuid.NewString()

	failed, err := paymentdomain.NewPayment(userID, walletID, paymentdomain.DirectionDeposit,
		"BANK_TRANSFER", decimal.NewFromInt(25), "USD", ref, "", nil)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	if err := failed.TransitionTo(paymentdomain.StatusProcessing); err != nil {
		t.Fatalf("TransitionTo PROCESSING: %v", err)
	}
	if err := failed.TransitionTo(paymentdomain.StatusFailed); err != nil {
		t.Fatalf("TransitionTo FAILED: %v", err)
	}
	if err := paymentpostgres.New(db).Create(ctx, failed); err != nil {
		t.Fatalf("seed failed payment: %v", err)
	}

	depositor := NewDepositor(NewSQLDepositStore(db))
	_, err = depositor.Deposit(ctx, DepositInput{
		UserID: userID, WalletID: walletID, Method: "BANK_TRANSFER",
		Amount: decimal.NewFromInt(25), Currency: "USD", Reference: ref,
	})
	if !errors.Is(err, ErrReferenceConflict) {
		t.Fatalf("reusing a FAILED reference: expected ErrReferenceConflict, got %v", err)
	}
	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("balance changed on rejected reuse: got %s, want 100", got)
	}

	// A new request with a new reference succeeds.
	if _, err := depositor.Deposit(ctx, DepositInput{
		UserID: userID, WalletID: walletID, Method: "BANK_TRANSFER",
		Amount: decimal.NewFromInt(25), Currency: "USD", Reference: "IDEM-" + uuid.NewString(),
	}); err != nil {
		t.Fatalf("new reference after failure: %v", err)
	}
	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(125)) {
		t.Fatalf("balance after new request: got %s, want 125", got)
	}
}
