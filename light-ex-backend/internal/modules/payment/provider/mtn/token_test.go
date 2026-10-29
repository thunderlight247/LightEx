package mtn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenManager_AcquiresAndCachesToken(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/collection/token/" {
			t.Errorf("expected path /collection/token/, got %s", r.URL.Path)
		}
		if r.Header.Get("Ocp-Apim-Subscription-Key") != "sub-key-1" {
			t.Errorf("unexpected subscription key: %s", r.Header.Get("Ocp-Apim-Subscription-Key"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "test-access-token",
			TokenType:   "access_token",
			ExpiresIn:   3600,
		})
	}))
	defer server.Close()

	creds := SubscriptionConfig{
		SubscriptionKey: "sub-key-1",
		APIUser:         "user-1",
		APIKey:          "key-1",
	}

	tm := NewTokenManager(server.URL, "collection", creds, server.Client())

	ctx := context.Background()

	// First call should hit the server
	token1, err := tm.Token(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token1 != "test-access-token" {
		t.Fatalf("expected test-access-token, got %s", token1)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// Second call should return cached token
	token2, err := tm.Token(ctx)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if token2 != "test-access-token" {
		t.Fatalf("expected cached token, got %s", token2)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected call count to remain 1, got %d", callCount)
	}
}

func TestTokenManager_RefreshesExpiredToken(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "token-v" + string(rune('0'+count)),
			TokenType:   "access_token",
			ExpiresIn:   60,
		})
	}))
	defer server.Close()

	creds := SubscriptionConfig{
		SubscriptionKey: "sub-key-1",
		APIUser:         "user-1",
		APIKey:          "key-1",
	}

	now := time.Now()
	tm := NewTokenManager(server.URL, "collection", creds, server.Client())
	tm.nowFunc = func() time.Time {
		return now
	}

	ctx := context.Background()
	token1, err := tm.Token(ctx)
	if err != nil {
		t.Fatalf("token 1: %v", err)
	}

	// Advance time past expiration buffer
	now = now.Add(50 * time.Second)

	token2, err := tm.Token(ctx)
	if err != nil {
		t.Fatalf("token 2: %v", err)
	}

	if token1 == token2 {
		t.Fatalf("expected refreshed token, got same %s", token1)
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
}

func TestTokenManager_MissingCredentials(t *testing.T) {
	tm := NewTokenManager("http://example.com", "collection", SubscriptionConfig{}, nil)
	_, err := tm.Token(context.Background())
	if !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials, got %v", err)
	}
}
