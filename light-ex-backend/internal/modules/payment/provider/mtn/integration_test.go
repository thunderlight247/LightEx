package mtn

import (
	"context"
	"testing"
	"time"
)

// TestMTNSandboxIntegration tests real communication with MTN MoMo Sandbox
// using the credentials in .env.mtn if they are configured.
func TestMTNSandboxIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := LoadConfigFromEnv()
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
