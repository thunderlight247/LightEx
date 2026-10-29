package mtn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var (
	ErrMissingCredentials = errors.New("mtn credentials missing for product")
	ErrTokenRequestFailed = errors.New("failed to acquire mtn access token")
)

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// TokenManager acquires and caches OAuth2 access tokens for an MTN product.
type TokenManager struct {
	mu          sync.RWMutex
	product     string // "collection" or "disbursement"
	baseURL     string
	creds       SubscriptionConfig
	httpClient  *http.Client
	cachedToken string
	expiresAt   time.Time
	nowFunc     func() time.Time // override in tests
}

// NewTokenManager creates a TokenManager for collection or disbursement.
func NewTokenManager(baseURL, product string, creds SubscriptionConfig, client *http.Client) *TokenManager {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &TokenManager{
		product:    product,
		baseURL:    baseURL,
		creds:      creds,
		httpClient: client,
		nowFunc:    time.Now,
	}
}

// Token returns a valid access token, reusing cached token if not expired.
func (tm *TokenManager) Token(ctx context.Context) (string, error) {
	if !tm.creds.IsConfigured() {
		return "", fmt.Errorf("%w: %s", ErrMissingCredentials, tm.product)
	}

	tm.mu.RLock()
	now := tm.nowFunc()
	if tm.cachedToken != "" && now.Add(30*time.Second).Before(tm.expiresAt) {
		token := tm.cachedToken
		tm.mu.RUnlock()
		return token, nil
	}
	tm.mu.RUnlock()

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Double check after acquiring write lock
	now = tm.nowFunc()
	if tm.cachedToken != "" && now.Add(30*time.Second).Before(tm.expiresAt) {
		return tm.cachedToken, nil
	}

	token, expiresIn, err := tm.fetchToken(ctx)
	if err != nil {
		return "", err
	}

	tm.cachedToken = token
	tm.expiresAt = now.Add(time.Duration(expiresIn) * time.Second)

	return token, nil
}

func (tm *TokenManager) fetchToken(ctx context.Context) (string, int, error) {
	url := fmt.Sprintf("%s/%s/token/", tm.baseURL, tm.product)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", 0, fmt.Errorf("create token request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString(
		[]byte(fmt.Sprintf("%s:%s", tm.creds.APIUser, tm.creds.APIKey)),
	)
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Ocp-Apim-Subscription-Key", tm.creds.SubscriptionKey)
	req.Header.Set("Content-Length", "0")

	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrTokenRequestFailed, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read token response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", 0, fmt.Errorf("%w: status %d, body: %s", ErrTokenRequestFailed, resp.StatusCode, string(body))
	}

	var res tokenResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return "", 0, fmt.Errorf("decode token response: %w", err)
	}

	if res.AccessToken == "" {
		return "", 0, fmt.Errorf("%w: empty access_token in response", ErrTokenRequestFailed)
	}

	expiresIn := res.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600 // fallback standard 1 hour
	}

	return res.AccessToken, expiresIn, nil
}
