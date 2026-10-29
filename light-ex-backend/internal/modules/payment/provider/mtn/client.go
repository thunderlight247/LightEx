package mtn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

const (
	ProviderName = "MTN_MOMO"

	PartyIDTypeMSISDN = "MSISDN"

	MTNStatusSuccessful = "SUCCESSFUL"
	MTNStatusFailed     = "FAILED"
	MTNStatusPending    = "PENDING"
)

var (
	ErrUnexpectedResponseStatus = errors.New("mtn unexpected response status")
	ErrStatusNotFound           = errors.New("mtn transaction status not found")
)

// Party describes a customer or merchant mobile wallet account.
type Party struct {
	PartyIDType string `json:"partyIdType"`
	PartyID     string `json:"partyId"`
}

// RequestToPayPayload is the JSON payload sent to initiate a collection.
type RequestToPayPayload struct {
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	ExternalID   string `json:"externalId"`
	Payer        Party  `json:"payer"`
	PayerMessage string `json:"payerMessage"`
	PayeeNote    string `json:"payeeNote"`
}

// TransferPayload is the JSON payload sent to initiate a disbursement.
type TransferPayload struct {
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	ExternalID   string `json:"externalId"`
	Payee        Party  `json:"payee"`
	PayerMessage string `json:"payerMessage"`
	PayeeNote    string `json:"payeeNote"`
}

// TransactionStatusResponse is returned by status lookup endpoints.
type TransactionStatusResponse struct {
	Amount                 string `json:"amount"`
	Currency               string `json:"currency"`
	FinancialTransactionID string `json:"financialTransactionId"`
	ExternalID             string `json:"externalId"`
	Payer                  *Party `json:"payer,omitempty"`
	Payee                  *Party `json:"payee,omitempty"`
	Status                 string `json:"status"`
	Reason                 any    `json:"reason,omitempty"`
}

// Client implements provider.Provider for MTN Mobile Money.
type Client struct {
	cfg              Config
	httpClient       *http.Client
	collectionTokens *TokenManager
	disburseTokens   *TokenManager
}

// Verify interface compliance at compile time.
var _ provider.Provider = (*Client)(nil)

// NewClient creates a new MTN MoMo provider client.
func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}
		httpClient = &http.Client{Timeout: timeout}
	}

	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
		collectionTokens: NewTokenManager(
			cfg.BaseURL,
			"collection",
			cfg.Collection,
			httpClient,
		),
		disburseTokens: NewTokenManager(
			cfg.BaseURL,
			"disbursement",
			cfg.Disbursement,
			httpClient,
		),
	}
}

// Name returns the provider identifier.
func (c *Client) Name() string {
	return ProviderName
}

// Collect initiates a Mobile Money deposit by requesting payment from customer phone.
func (c *Client) Collect(ctx context.Context, req provider.Request) (provider.Result, error) {
	if err := provider.Validate(req); err != nil {
		return provider.Result{}, err
	}

	token, err := c.collectionTokens.Token(ctx)
	if err != nil {
		return provider.Result{}, fmt.Errorf("collection token: %w", err)
	}

	referenceID := uuid.New().String()

	payload := RequestToPayPayload{
		Amount:     req.Amount.StringFixed(2),
		Currency:   req.Currency,
		ExternalID: req.Reference,
		Payer: Party{
			PartyIDType: PartyIDTypeMSISDN,
			PartyID:     NormalizePhone(req.Phone),
		},
		PayerMessage: "Deposit to LightEx",
		PayeeNote:    "LightEx Deposit " + req.Reference,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return provider.Result{}, fmt.Errorf("marshal requesttopay payload: %w", err)
	}

	url := fmt.Sprintf("%s/collection/v1_0/requesttopay", c.cfg.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return provider.Result{}, fmt.Errorf("create requesttopay request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("X-Reference-Id", referenceID)
	httpReq.Header.Set("X-Target-Environment", c.cfg.Environment)
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", c.cfg.Collection.SubscriptionKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.CallbackURL != "" {
		httpReq.Header.Set("X-Callback-Url", c.cfg.CallbackURL)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return provider.Result{}, fmt.Errorf("requesttopay post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return provider.Result{}, fmt.Errorf("%w: status %d, body: %s", ErrUnexpectedResponseStatus, resp.StatusCode, string(respBody))
	}

	return provider.Result{
		ProviderReference: referenceID,
		Status:            provider.StatusPending,
	}, nil
}

// Disburse initiates a withdrawal by sending money to customer phone.
func (c *Client) Disburse(ctx context.Context, req provider.Request) (provider.Result, error) {
	if err := provider.Validate(req); err != nil {
		return provider.Result{}, err
	}

	token, err := c.disburseTokens.Token(ctx)
	if err != nil {
		return provider.Result{}, fmt.Errorf("disbursement token: %w", err)
	}

	referenceID := uuid.New().String()

	payload := TransferPayload{
		Amount:     req.Amount.StringFixed(2),
		Currency:   req.Currency,
		ExternalID: req.Reference,
		Payee: Party{
			PartyIDType: PartyIDTypeMSISDN,
			PartyID:     NormalizePhone(req.Phone),
		},
		PayerMessage: "Withdrawal from LightEx",
		PayeeNote:    "LightEx Withdrawal " + req.Reference,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return provider.Result{}, fmt.Errorf("marshal transfer payload: %w", err)
	}

	url := fmt.Sprintf("%s/disbursement/v1_0/transfer", c.cfg.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return provider.Result{}, fmt.Errorf("create transfer request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("X-Reference-Id", referenceID)
	httpReq.Header.Set("X-Target-Environment", c.cfg.Environment)
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", c.cfg.Disbursement.SubscriptionKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.CallbackURL != "" {
		httpReq.Header.Set("X-Callback-Url", c.cfg.CallbackURL)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return provider.Result{}, fmt.Errorf("transfer post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return provider.Result{}, fmt.Errorf("%w: status %d, body: %s", ErrUnexpectedResponseStatus, resp.StatusCode, string(respBody))
	}

	return provider.Result{
		ProviderReference: referenceID,
		Status:            provider.StatusPending,
	}, nil
}

// CheckCollectionStatus polls the status of a requesttopay collection.
func (c *Client) CheckCollectionStatus(ctx context.Context, referenceID string) (provider.Result, error) {
	if strings.TrimSpace(referenceID) == "" {
		return provider.Result{}, errors.New("referenceID is required")
	}

	token, err := c.collectionTokens.Token(ctx)
	if err != nil {
		return provider.Result{}, fmt.Errorf("collection token: %w", err)
	}

	url := fmt.Sprintf("%s/collection/v1_0/requesttopay/%s", c.cfg.BaseURL, referenceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return provider.Result{}, fmt.Errorf("create check status request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Target-Environment", c.cfg.Environment)
	req.Header.Set("Ocp-Apim-Subscription-Key", c.cfg.Collection.SubscriptionKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return provider.Result{}, fmt.Errorf("check collection status request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return provider.Result{}, fmt.Errorf("read status response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return provider.Result{}, ErrStatusNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return provider.Result{}, fmt.Errorf("%w: status %d, body: %s", ErrUnexpectedResponseStatus, resp.StatusCode, string(body))
	}

	var statusRes TransactionStatusResponse
	if err := json.Unmarshal(body, &statusRes); err != nil {
		return provider.Result{}, fmt.Errorf("decode status response: %w", err)
	}

	return provider.Result{
		ProviderReference: referenceID,
		Status:            MapMTNStatus(statusRes.Status),
	}, nil
}

// CheckDisbursementStatus polls the status of a disbursement transfer.
func (c *Client) CheckDisbursementStatus(ctx context.Context, referenceID string) (provider.Result, error) {
	if strings.TrimSpace(referenceID) == "" {
		return provider.Result{}, errors.New("referenceID is required")
	}

	token, err := c.disburseTokens.Token(ctx)
	if err != nil {
		return provider.Result{}, fmt.Errorf("disbursement token: %w", err)
	}

	url := fmt.Sprintf("%s/disbursement/v1_0/transfer/%s", c.cfg.BaseURL, referenceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return provider.Result{}, fmt.Errorf("create check status request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Target-Environment", c.cfg.Environment)
	req.Header.Set("Ocp-Apim-Subscription-Key", c.cfg.Disbursement.SubscriptionKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return provider.Result{}, fmt.Errorf("check disbursement status request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return provider.Result{}, fmt.Errorf("read status response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return provider.Result{}, ErrStatusNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return provider.Result{}, fmt.Errorf("%w: status %d, body: %s", ErrUnexpectedResponseStatus, resp.StatusCode, string(body))
	}

	var statusRes TransactionStatusResponse
	if err := json.Unmarshal(body, &statusRes); err != nil {
		return provider.Result{}, fmt.Errorf("decode status response: %w", err)
	}

	return provider.Result{
		ProviderReference: referenceID,
		Status:            MapMTNStatus(statusRes.Status),
	}, nil
}

// MapMTNStatus maps MTN's status string to provider.Status.
func MapMTNStatus(mtnStatus string) provider.Status {
	switch strings.ToUpper(strings.TrimSpace(mtnStatus)) {
	case MTNStatusSuccessful:
		return provider.StatusSucceeded
	case MTNStatusFailed:
		return provider.StatusFailed
	default:
		return provider.StatusPending
	}
}

// NormalizePhone cleans phone numbers into digits-only format required by MTN MSISDN.
func NormalizePhone(phone string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	return cleaned
}
