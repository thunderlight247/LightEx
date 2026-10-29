package crypto

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

func TestCryptoProvider_CollectAndDisburse(t *testing.T) {
	cp := NewProvider()
	ctx := context.Background()

	if cp.Name() != ProviderName {
		t.Errorf("expected %s, got %s", ProviderName, cp.Name())
	}

	// Test Collect
	collReq := provider.Request{
		Reference: "DEP-CRYP-101",
		Amount:    decimal.NewFromFloat(150.50),
		Currency:  "USDT",
		Phone:     "crypto",
	}

	collRes, err := cp.Collect(ctx, collReq)
	if err != nil {
		t.Fatalf("unexpected Collect error: %v", err)
	}
	if collRes.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v", collRes.Status)
	}

	statusRes, err := cp.CheckStatus(ctx, collRes.ProviderReference)
	if err != nil {
		t.Fatalf("unexpected CheckStatus error: %v", err)
	}
	if statusRes.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v", statusRes.Status)
	}

	// Update status
	_ = cp.SetStatus(collRes.ProviderReference, provider.StatusSucceeded)
	statusRes2, _ := cp.CheckStatus(ctx, collRes.ProviderReference)
	if statusRes2.Status != provider.StatusSucceeded {
		t.Errorf("expected StatusSucceeded, got %v", statusRes2.Status)
	}

	// Test Disburse
	disbReq := provider.Request{
		Reference: "WTH-CRYP-202",
		Amount:    decimal.NewFromFloat(50.25),
		Currency:  "USDT",
		Phone:     "crypto",
	}

	disbRes, err := cp.Disburse(ctx, disbReq)
	if err != nil {
		t.Fatalf("unexpected Disburse error: %v", err)
	}
	if disbRes.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v", disbRes.Status)
	}
}

func TestValidateAddress(t *testing.T) {
	// EVM address
	if err := ValidateAddress("ETH", "0x742d35Cc6634C0532925a3b844Bc454e4438f44e"); err != nil {
		t.Errorf("expected valid EVM address, got %v", err)
	}
	if err := ValidateAddress("ETH", "0xinvalid"); err == nil {
		t.Errorf("expected error for invalid EVM address")
	}

	// TRON address
	if err := ValidateAddress("TRC20", "TLyqz4YmmYiiVcfUbXdURiaDAEVNz22B4j"); err != nil {
		t.Errorf("expected valid TRON address, got %v", err)
	}
	if err := ValidateAddress("TRC20", "0x123"); err == nil {
		t.Errorf("expected error for invalid TRON address")
	}

	// BTC address
	if err := ValidateAddress("BTC", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"); err != nil {
		t.Errorf("expected valid BTC address, got %v", err)
	}
}
