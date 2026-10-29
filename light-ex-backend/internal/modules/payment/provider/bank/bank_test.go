package bank

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

func TestBankProvider_CollectAndDisburse(t *testing.T) {
	bp := NewProvider()
	ctx := context.Background()

	if bp.Name() != ProviderName {
		t.Errorf("expected %s, got %s", ProviderName, bp.Name())
	}

	// Test Collect
	collReq := provider.Request{
		Reference: "DEP-BNK-101",
		Amount:    decimal.NewFromInt(500),
		Currency:  "GHS",
		Phone:     "0240000000",
	}

	collRes, err := bp.Collect(ctx, collReq)
	if err != nil {
		t.Fatalf("unexpected Collect error: %v", err)
	}
	if collRes.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v", collRes.Status)
	}
	if collRes.ProviderReference == "" {
		t.Errorf("expected non-empty ProviderReference")
	}

	// Check status
	statusRes, err := bp.CheckStatus(ctx, collRes.ProviderReference)
	if err != nil {
		t.Fatalf("unexpected CheckStatus error: %v", err)
	}
	if statusRes.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v", statusRes.Status)
	}

	// Update status
	if err := bp.SetStatus(collRes.ProviderReference, provider.StatusSucceeded); err != nil {
		t.Fatalf("unexpected SetStatus error: %v", err)
	}
	statusRes2, _ := bp.CheckStatus(ctx, collRes.ProviderReference)
	if statusRes2.Status != provider.StatusSucceeded {
		t.Errorf("expected StatusSucceeded, got %v", statusRes2.Status)
	}

	// Test Disburse
	disbReq := provider.Request{
		Reference: "WTH-BNK-202",
		Amount:    decimal.NewFromInt(250),
		Currency:  "GHS",
		Phone:     "0240000000",
	}

	disbRes, err := bp.Disburse(ctx, disbReq)
	if err != nil {
		t.Fatalf("unexpected Disburse error: %v", err)
	}
	if disbRes.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v", disbRes.Status)
	}
}

func TestAccountDetails_Validate(t *testing.T) {
	valid := AccountDetails{
		AccountNumber: "1234567890",
		BankCode:      "GCB",
		BankName:      "GCB Bank",
		AccountName:   "John Doe",
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid, got %v", err)
	}

	invalidAcc := valid
	invalidAcc.AccountNumber = "123"
	if err := invalidAcc.Validate(); err == nil {
		t.Errorf("expected error for short account number")
	}

	invalidBank := valid
	invalidBank.BankCode = ""
	if err := invalidBank.Validate(); err == nil {
		t.Errorf("expected error for empty bank code")
	}
}
