package service

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

func TestSettle_MovesFundsAndPostsBothSides(t *testing.T) {
	db := getDepositTestDB(t)
	_, fromWallet := seedUserWallet(t, db, "100")
	_, toWallet := seedUserWallet(t, db, "5")

	res, err := NewSettler(NewSQLDepositStore(db)).Settle(context.Background(), SettlementInput{
		FromWalletID: fromWallet,
		ToWalletID:   toWallet,
		Amount:       decimal.NewFromInt(40),
		Currency:     "USD",
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}

	if got := readBalance(t, db, fromWallet); !got.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("source balance: got %s, want 60", got)
	}
	if got := readBalance(t, db, toWallet); !got.Equal(decimal.NewFromInt(45)) {
		t.Fatalf("destination balance: got %s, want 45", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ledger_entries WHERE transaction_id = $1 AND entry_type = 'DEBIT'`, res.From.ID); n != 1 {
		t.Fatalf("expected 1 DEBIT entry for source, got %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ledger_entries WHERE transaction_id = $1 AND entry_type = 'CREDIT'`, res.To.ID); n != 1 {
		t.Fatalf("expected 1 CREDIT entry for destination, got %d", n)
	}
}

func TestSettle_InsufficientFundsRollsBackEverything(t *testing.T) {
	db := getDepositTestDB(t)
	_, fromWallet := seedUserWallet(t, db, "10")
	_, toWallet := seedUserWallet(t, db, "5")

	_, err := NewSettler(NewSQLDepositStore(db)).Settle(context.Background(), SettlementInput{
		FromWalletID: fromWallet,
		ToWalletID:   toWallet,
		Amount:       decimal.NewFromInt(40),
		Currency:     "USD",
	})
	if err == nil {
		t.Fatal("expected insufficient funds error, got nil")
	}

	if got := readBalance(t, db, fromWallet); !got.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("source balance changed: got %s, want 10", got)
	}
	if got := readBalance(t, db, toWallet); !got.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("destination balance changed: got %s, want 5", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM transactions WHERE wallet_id IN ($1, $2)`, fromWallet, toWallet); n != 0 {
		t.Fatalf("expected no transactions, got %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ledger_entries WHERE wallet_id IN ($1, $2)`, fromWallet, toWallet); n != 0 {
		t.Fatalf("expected no ledger entries, got %d", n)
	}
}
