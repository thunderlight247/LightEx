package mtn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"

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

type callbackTestFinder struct {
	payment *paymentdomain.Payment
	err     error
}

func (f callbackTestFinder) FindByReference(_ context.Context, ref string) (*paymentdomain.Payment, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.payment == nil || f.payment.Reference != ref {
		return nil, errors.New("not found")
	}
	return f.payment, nil
}

func TestVerifyCallbackWithMTNStatusLookup(t *testing.T) {
	for _, direction := range []paymentdomain.Direction{paymentdomain.DirectionDeposit, paymentdomain.DirectionWithdrawal} {
		for _, status := range []string{MTNStatusPending, MTNStatusSuccessful, MTNStatusFailed} {
			t.Run(string(direction)+"/"+status, func(t *testing.T) {
				ref := uuid.NewString()
				payment := &paymentdomain.Payment{ID: uuid.New(), Reference: "PAY-123", ProviderReference: ref, Method: ProviderName, Direction: direction, Status: paymentdomain.StatusProcessing, Amount: decimal.RequireFromString("50.00"), Currency: "GHS"}
				callback := &CallbackNotification{ExternalID: payment.Reference, FinancialTransactionID: "FIN-1", Amount: "50.0", Currency: "GHS", Status: status}
				remote := TransactionStatusResponse{ExternalID: payment.Reference, FinancialTransactionID: "FIN-1", Amount: "50.00", Currency: "GHS", Status: status}
				var httpStatus = http.StatusOK
				var expectedPath, tokenPath, subscription string
				if direction == paymentdomain.DirectionDeposit {
					expectedPath = "/collection/v1_0/requesttopay/" + ref
					tokenPath = "/collection/token/"
					subscription = "coll-sub-key"
				} else {
					expectedPath = "/disbursement/v1_0/transfer/" + ref
					tokenPath = "/disbursement/token/"
					subscription = "disb-sub-key"
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case tokenPath:
						_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "token", ExpiresIn: 3600})
					case expectedPath:
						if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Ocp-Apim-Subscription-Key") != subscription {
							t.Errorf("incorrect authenticated status request")
						}
						w.WriteHeader(httpStatus)
						_ = json.NewEncoder(w).Encode(remote)
					default:
						t.Errorf("unexpected path: %s", r.URL.Path)
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				client := NewClient(dummyConfig(server.URL, server.Client()))
				verify := func() (VerifiedCallback, error) {
					return VerifyCallback(context.Background(), callbackTestFinder{payment: payment}, client, callback)
				}
				verified, err := verify()
				if err != nil || verified.PaymentID != payment.ID || verified.ProviderReference != ref || verified.Status != MapMTNStatus(status) {
					t.Fatalf("verification: %+v %v", verified, err)
				}
				if payment.Status != paymentdomain.StatusProcessing {
					t.Fatal("verification modified payment")
				}
				tests := []struct {
					name   string
					change func()
					reset  func()
				}{
					{"callback amount", func() { callback.Amount = "51" }, func() { callback.Amount = "50.0" }},
					{"callback currency", func() { callback.Currency = "USD" }, func() { callback.Currency = "GHS" }},
					{"callback status", func() { callback.Status = "UNKNOWN" }, func() { callback.Status = status }},
					{"callback transaction id", func() { callback.FinancialTransactionID = "wrong" }, func() { callback.FinancialTransactionID = "FIN-1" }},
					{"provider amount", func() { remote.Amount = "51" }, func() { remote.Amount = "50.00" }},
					{"provider currency", func() { remote.Currency = "USD" }, func() { remote.Currency = "GHS" }},
					{"provider reference", func() { remote.ExternalID = "other" }, func() { remote.ExternalID = payment.Reference }},
					{"provider unknown status", func() { remote.Status = "UNKNOWN" }, func() { remote.Status = status }},
					{"provider mismatched status", func() {
						if status == MTNStatusSuccessful {
							remote.Status = MTNStatusFailed
						} else {
							remote.Status = MTNStatusSuccessful
						}
					}, func() { remote.Status = status }},
					{"missing provider reference", func() { payment.ProviderReference = "" }, func() { payment.ProviderReference = ref }},
					{"wrong payment method", func() { payment.Method = "BANK" }, func() { payment.Method = ProviderName }},
					{"terminal payment", func() { payment.Status = paymentdomain.StatusCompleted }, func() { payment.Status = paymentdomain.StatusProcessing }},
				}
				for _, tt := range tests {
					t.Run(tt.name, func(t *testing.T) {
						tt.change()
						defer tt.reset()
						if _, err := verify(); !errors.Is(err, ErrCallbackVerification) {
							t.Fatalf("expected verification error, got %v", err)
						}
					})
				}
				httpStatus = http.StatusInternalServerError
				if _, err := verify(); !errors.Is(err, ErrUnexpectedResponseStatus) {
					t.Fatalf("provider error: %v", err)
				}
			})
		}
	}
}

func TestLookupTransactionRejectsInvalidReferenceAndProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token/") {
			_, _ = fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		_, _ = fmt.Fprint(w, strings.Repeat("x", 65537))
	}))
	defer server.Close()
	client := NewClient(dummyConfig(server.URL, server.Client()))
	if _, err := client.LookupTransaction(context.Background(), "DEPOSIT", "../../other"); err == nil {
		t.Fatal("invalid reference accepted")
	}
	if _, err := client.LookupTransaction(context.Background(), "OTHER", uuid.NewString()); err == nil {
		t.Fatal("invalid direction accepted")
	}
	if _, err := client.LookupTransaction(context.Background(), "DEPOSIT", uuid.NewString()); err == nil {
		t.Fatal("oversized response accepted")
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
