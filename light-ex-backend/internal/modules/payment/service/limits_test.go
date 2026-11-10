package service

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

func TestInitiateDeposit_OverLimitRejectedBeforeProviderCall(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "prov-ref", Status: provider.StatusPending}}
	collector := NewCollector(NewSQLDepositStore(db), fake).WithLimits(Limits{MaxDeposit: decimal.NewFromInt(20)})

	_, err := collector.InitiateDeposit(ctx, baseCollectionInput(userID, walletID)) // amount 30 > 20
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected ErrLimitExceeded, got %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("provider must not be called for an over-limit deposit, got %d calls", fake.calls)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM payments WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("over-limit deposit must not persist a payment, got %d", n)
	}
}

func TestInitiateWithdrawal_OverLimitRejectedBeforeReservation(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "prov-ref", Status: provider.StatusPending}}
	collector := NewCollector(NewSQLDepositStore(db), fake).WithLimits(Limits{MaxWithdrawal: decimal.NewFromInt(20)})

	_, err := collector.InitiateWithdrawal(ctx, baseCollectionInput(userID, walletID)) // amount 30 > 20
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected ErrLimitExceeded, got %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("provider must not be called for an over-limit withdrawal, got %d calls", fake.calls)
	}
	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("over-limit withdrawal must not lock funds, balance is %s", got)
	}
}

func TestInitiateDeposit_AtLimitAccepted(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "prov-ref", Status: provider.StatusPending}}
	collector := NewCollector(NewSQLDepositStore(db), fake).WithLimits(Limits{MaxDeposit: decimal.NewFromInt(30)})

	if _, err := collector.InitiateDeposit(ctx, baseCollectionInput(userID, walletID)); err != nil { // amount 30 == 30
		t.Fatalf("amount equal to the limit should be accepted: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected one provider call, got %d", fake.calls)
	}
}

func TestInitiateDeposit_ZeroLimitMeansUnlimited(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	fake := &fakeInitiator{result: provider.Result{ProviderReference: "prov-ref", Status: provider.StatusPending}}
	collector := NewCollector(NewSQLDepositStore(db), fake).WithLimits(Limits{})

	if _, err := collector.InitiateDeposit(ctx, baseCollectionInput(userID, walletID)); err != nil {
		t.Fatalf("zero limit should not restrict deposits: %v", err)
	}
}
