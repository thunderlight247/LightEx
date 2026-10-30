package mtn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

func loadSandboxConfig(t *testing.T) Config {
	t.Helper()
	// go test runs from the package directory; .env.mtn lives at the repo root.
	_ = godotenv.Load("../../../../../.env.mtn")
	cfg := LoadConfigFromEnv()
	if !cfg.Collection.IsConfigured() {
		t.Skip("skipping MTN sandbox integration test: collection credentials not set in .env.mtn or environment")
	}
	return cfg
}

// TestMTNSandboxIntegration tests real communication with MTN MoMo Sandbox
// using the credentials in .env.mtn if they are configured.
func TestMTNSandboxIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := loadSandboxConfig(t)
	if !cfg.Collection.IsConfigured() {
		t.Skip("skipping MTN sandbox integration test: collection credentials not set in .env.mtn or environment")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := NewClient(cfg)

	// Test collection token generation
	token, err := client.collectionTokens.Token(ctx)
	if err != nil {
		t.Fatalf("failed to acquire real collection token from MTN Sandbox: %v", err)
	}

	if token == "" {
		t.Fatalf("acquired token is empty")
	}

	t.Logf("Successfully acquired real MTN Collection OAuth2 token (length: %d)", len(token))

	if cfg.Disbursement.IsConfigured() {
		disbToken, err := client.disburseTokens.Token(ctx)
		if err != nil {
			t.Fatalf("failed to acquire real disbursement token from MTN Sandbox: %v", err)
		}
		if disbToken == "" {
			t.Fatalf("acquired disbursement token is empty")
		}
		t.Logf("Successfully acquired real MTN Disbursement OAuth2 token (length: %d)", len(disbToken))
	}
}

// TestMTNSandboxCollectionContract runs a real sandbox requesttopay collection
// and then reads it back through LookupTransaction, validating that the live
// response carries all fields strict callback verification depends on
// (amount, currency, externalId, financialTransactionId, status).
// No real money moves: this only creates records in the MTN sandbox.
func TestMTNSandboxCollectionContract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg := loadSandboxConfig(t)
	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	reference := "E2E-" + uuid.NewString()
	// The sandbox target environment rejects GHS with INVALID_CURRENCY; the
	// sandbox's supported currency carries the same contract shape we verify.
	res, err := client.Collect(ctx, provider.Request{
		Reference: reference,
		Amount:    decimal.NewFromInt(5),
		Currency:  "EUR",
		Phone:     "233240000000",
	})
	if err != nil {
		t.Fatalf("real sandbox requesttopay: %v", err)
	}
	if res.ProviderReference == "" || res.Status != provider.StatusPending {
		t.Fatalf("unexpected collect result: %+v", res)
	}
	t.Logf("sandbox requesttopay accepted: reference=%s providerReference=%s", reference, res.ProviderReference)

	// Sandbox records can take a beat to become readable; retry briefly on 404.
	var record TransactionStatusResponse
	deadline := time.Now().Add(20 * time.Second)
	for {
		record, err = client.LookupTransaction(ctx, "DEPOSIT", res.ProviderReference)
		if err == nil || (time.Now().Before(deadline) && errors.Is(err, ErrStatusNotFound)) {
			if err == nil {
				break
			}
			time.Sleep(time.Second)
			continue
		}
		t.Fatalf("real sandbox status lookup: %v", err)
	}

	if record.ExternalID != reference {
		t.Errorf("sandbox externalId mismatch: got %q want %q", record.ExternalID, reference)
	}
	amount, aerr := decimal.NewFromString(record.Amount)
	if aerr != nil || !amount.Equal(decimal.NewFromInt(5)) {
		t.Errorf("sandbox amount mismatch: got %q (%v)", record.Amount, aerr)
	}
	if record.Currency != "EUR" {
		t.Errorf("sandbox currency mismatch: got %q", record.Currency)
	}
	if record.FinancialTransactionID == "" {
		t.Errorf("sandbox financialTransactionId is empty")
	}
	switch record.Status {
	case MTNStatusPending, MTNStatusSuccessful, MTNStatusFailed:
		t.Logf("sandbox record decoded fully: providerReference=%s financialTransactionId=%s status=%s", res.ProviderReference, record.FinancialTransactionID, record.Status)
	default:
		t.Errorf("unknown sandbox status %q", record.Status)
	}
}

// TestMTNSandboxDisbursementContract runs a real sandbox transfer and then
// reads it back through LookupTransaction, validating that the live disbursement
// response carries all fields strict callback verification depends on.
// No real money moves: sandbox transfers do not pay anyone and a FAILED status
// (for example an unfunded sandbox wallet) still satisfies the contract check.
func TestMTNSandboxDisbursementContract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg := loadSandboxConfig(t)
	if !cfg.Disbursement.IsConfigured() {
		t.Skip("skipping MTN sandbox disbursement test: disbursement credentials not configured")
	}
	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	reference := "E2E-" + uuid.NewString()
	res, err := client.Disburse(ctx, provider.Request{
		Reference: reference,
		Amount:    decimal.NewFromInt(5),
		Currency:  "EUR",
		Phone:     "233240000000",
	})
	if err != nil {
		t.Fatalf("real sandbox transfer: %v", err)
	}
	if res.ProviderReference == "" || res.Status != provider.StatusPending {
		t.Fatalf("unexpected disburse result: %+v", res)
	}
	t.Logf("sandbox transfer accepted: reference=%s providerReference=%s", reference, res.ProviderReference)

	// Sandbox records can take a beat to become readable; retry briefly on 404.
	var record TransactionStatusResponse
	deadline := time.Now().Add(20 * time.Second)
	for {
		record, err = client.LookupTransaction(ctx, "WITHDRAWAL", res.ProviderReference)
		if err == nil || (time.Now().Before(deadline) && errors.Is(err, ErrStatusNotFound)) {
			if err == nil {
				break
			}
			time.Sleep(time.Second)
			continue
		}
		t.Fatalf("real sandbox status lookup: %v", err)
	}

	if record.ExternalID != reference {
		t.Errorf("sandbox externalId mismatch: got %q want %q", record.ExternalID, reference)
	}
	amount, aerr := decimal.NewFromString(record.Amount)
	if aerr != nil || !amount.Equal(decimal.NewFromInt(5)) {
		t.Errorf("sandbox amount mismatch: got %q (%v)", record.Amount, aerr)
	}
	if record.Currency != "EUR" {
		t.Errorf("sandbox currency mismatch: got %q", record.Currency)
	}
	switch record.Status {
	case MTNStatusPending, MTNStatusSuccessful:
		if record.FinancialTransactionID == "" {
			t.Errorf("sandbox financialTransactionId empty for status %q", record.Status)
		}
		t.Logf("sandbox disbursement record decoded fully: providerReference=%s financialTransactionId=%s status=%s", res.ProviderReference, record.FinancialTransactionID, record.Status)
	case MTNStatusFailed:
		if reason, ok := record.Reason.(string); ok && record.Reason != nil {
			t.Logf("sandbox disbursement record FAILED with reason %q (sandbox wallet state, contract still valid)", reason)
		} else {
			t.Logf("sandbox disbursement record FAILED with reason %+v (sandbox wallet state, contract still valid)", record.Reason)
		}
	default:
		t.Errorf("unknown sandbox disbursement status %q", record.Status)
	}
}
