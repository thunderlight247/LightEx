package mtn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

func dummyConfig(serverURL string, client *http.Client) Config {
	return Config{
		BaseURL:     serverURL,
		Environment: "sandbox",
		Collection: SubscriptionConfig{
			SubscriptionKey: "coll-sub-key",
			APIUser:         "coll-user",
			APIKey:          "coll-key",
		},
		Disbursement: SubscriptionConfig{
			SubscriptionKey: "disb-sub-key",
			APIUser:         "disb-user",
			APIKey:          "disb-key",
		},
		CallbackURL: "https://example.com/callback",
		HTTPClient:  client,
	}
}

func TestClient_Collect_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/collection/token/":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "coll-token",
				ExpiresIn:   3600,
			})
		case "/collection/v1_0/requesttopay":
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer coll-token" {
				t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("X-Reference-Id") == "" {
				t.Errorf("missing X-Reference-Id header")
			}
			if r.Header.Get("X-Target-Environment") != "sandbox" {
				t.Errorf("unexpected target env: %s", r.Header.Get("X-Target-Environment"))
			}
			if r.Header.Get("Ocp-Apim-Subscription-Key") != "coll-sub-key" {
				t.Errorf("unexpected subscription key")
			}
			if r.Header.Get("X-Callback-Url") != "https://example.com/callback" {
				t.Errorf("unexpected callback url: %s", r.Header.Get("X-Callback-Url"))
			}

			var payload RequestToPayPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode payload: %v", err)
			}
			if payload.Amount != "50.00" || payload.Currency != "GHS" || payload.ExternalID != "DEP-1" {
				t.Errorf("unexpected payload values: %+v", payload)
			}
			if payload.Payer.PartyID != "233240000000" {
				t.Errorf("unexpected payer phone: %s", payload.Payer.PartyID)
			}

			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(dummyConfig(server.URL, server.Client()))

	res, err := client.Collect(context.Background(), provider.Request{
		Reference: "DEP-1",
		Amount:    decimal.NewFromFloat(50.00),
		Currency:  "GHS",
		Phone:     "+233-24-000-0000",
	})
	if err != nil {
		t.Fatalf("unexpected Collect error: %v", err)
	}

	if res.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v", res.Status)
	}
	if res.ProviderReference == "" {
		t.Errorf("expected non-empty ProviderReference")
	}
}

func TestClient_Disburse_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/disbursement/token/":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "disb-token",
				ExpiresIn:   3600,
			})
		case "/disbursement/v1_0/transfer":
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer disb-token" {
				t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("X-Reference-Id") == "" {
				t.Errorf("missing X-Reference-Id header")
			}
			if r.Header.Get("Ocp-Apim-Subscription-Key") != "disb-sub-key" {
				t.Errorf("unexpected subscription key")
			}

			var payload TransferPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode transfer: %v", err)
			}
			if payload.Amount != "25.00" || payload.Currency != "GHS" || payload.ExternalID != "WTH-1" {
				t.Errorf("unexpected payload: %+v", payload)
			}

			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(dummyConfig(server.URL, server.Client()))

	res, err := client.Disburse(context.Background(), provider.Request{
		Reference: "WTH-1",
		Amount:    decimal.NewFromFloat(25.00),
		Currency:  "GHS",
		Phone:     "0240000000",
	})
	if err != nil {
		t.Fatalf("unexpected Disburse error: %v", err)
	}

	if res.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v", res.Status)
	}
	if res.ProviderReference == "" {
		t.Errorf("expected non-empty ProviderReference")
	}
}

func TestClient_CheckCollectionStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/collection/token/":
			_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: 3600})
		case "/collection/v1_0/requesttopay/ref-success":
			_ = json.NewEncoder(w).Encode(TransactionStatusResponse{
				Status:                 "SUCCESSFUL",
				FinancialTransactionID: "fin-123",
			})
		case "/collection/v1_0/requesttopay/ref-failed":
			_ = json.NewEncoder(w).Encode(TransactionStatusResponse{
				Status: "FAILED",
			})
		case "/collection/v1_0/requesttopay/ref-pending":
			_ = json.NewEncoder(w).Encode(TransactionStatusResponse{
				Status: "PENDING",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(dummyConfig(server.URL, server.Client()))
	ctx := context.Background()

	// Successful
	res1, err := client.CheckCollectionStatus(ctx, "ref-success")
	if err != nil || res1.Status != provider.StatusSucceeded {
		t.Errorf("expected StatusSucceeded, got %v, err %v", res1.Status, err)
	}

	// Failed
	res2, err := client.CheckCollectionStatus(ctx, "ref-failed")
	if err != nil || res2.Status != provider.StatusFailed {
		t.Errorf("expected StatusFailed, got %v, err %v", res2.Status, err)
	}

	// Pending
	res3, err := client.CheckCollectionStatus(ctx, "ref-pending")
	if err != nil || res3.Status != provider.StatusPending {
		t.Errorf("expected StatusPending, got %v, err %v", res3.Status, err)
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"+233240000000", "233240000000"},
		{"024 123 4567", "0241234567"},
		{"+(233) 24-123-4567", "233241234567"},
	}

	for _, c := range cases {
		out := NormalizePhone(c.input)
		if out != c.expected {
			t.Errorf("input %s: expected %s, got %s", c.input, c.expected, out)
		}
	}
}
