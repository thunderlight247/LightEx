package bank

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

const (
	ProviderName = "BANK_TRANSFER"
)

var (
	ErrInvalidAccountNumber = errors.New("bank account number is required and must be alphanumeric")
	ErrInvalidBankCode      = errors.New("bank code is required")
	ErrInvalidAccountName   = errors.New("account name is required")
	ErrTransferNotFound     = errors.New("bank transfer not found")
)

// AccountDetails specifies a customer's bank destination or origin.
type AccountDetails struct {
	AccountNumber string
	BankCode      string
	BankName      string
	AccountName   string
}

// Validate checks that the bank account details are well-formed.
func (a AccountDetails) Validate() error {
	acc := strings.TrimSpace(a.AccountNumber)
	if acc == "" || len(acc) < 5 || len(acc) > 34 {
		return ErrInvalidAccountNumber
	}
	if strings.TrimSpace(a.BankCode) == "" {
		return ErrInvalidBankCode
	}
	return nil
}

// TransferRecord stores internal/mock bank settlement records.
type TransferRecord struct {
	Reference         string
	ProviderReference string
	Amount            string
	Currency          string
	Direction         string // DEPOSIT or WITHDRAWAL
	Status            provider.Status
	Account           AccountDetails
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Provider implements provider.Provider for bank transfers (e.g. GhIPSS Instant Pay, ACH, wire).
type Provider struct {
	mu        sync.RWMutex
	transfers map[string]*TransferRecord
}

var _ provider.Provider = (*Provider)(nil)

// NewProvider creates a new Bank Transfer provider.
func NewProvider() *Provider {
	return &Provider{
		transfers: make(map[string]*TransferRecord),
	}
}

func (p *Provider) Name() string {
	return ProviderName
}

// Collect initiates a bank deposit. In bank transfers, this provisions a virtual account or
// registers the reference for inbound wire reconciliation.
func (p *Provider) Collect(ctx context.Context, req provider.Request) (provider.Result, error) {
	if err := provider.Validate(req); err != nil {
		return provider.Result{}, err
	}

	providerRef := "BNK-DEP-" + uuid.New().String()[:12]

	p.mu.Lock()
	p.transfers[providerRef] = &TransferRecord{
		Reference:         req.Reference,
		ProviderReference: providerRef,
		Amount:            req.Amount.StringFixed(2),
		Currency:          req.Currency,
		Direction:         "DEPOSIT",
		Status:            provider.StatusPending,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	p.mu.Unlock()

	return provider.Result{
		ProviderReference: providerRef,
		Status:            provider.StatusPending,
	}, nil
}

// Disburse initiates an outbound bank transfer / EFT to the customer's bank account.
func (p *Provider) Disburse(ctx context.Context, req provider.Request) (provider.Result, error) {
	if err := provider.Validate(req); err != nil {
		return provider.Result{}, err
	}

	providerRef := "BNK-WTH-" + uuid.New().String()[:12]

	p.mu.Lock()
	p.transfers[providerRef] = &TransferRecord{
		Reference:         req.Reference,
		ProviderReference: providerRef,
		Amount:            req.Amount.StringFixed(2),
		Currency:          req.Currency,
		Direction:         "WITHDRAWAL",
		Status:            provider.StatusPending,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	p.mu.Unlock()

	return provider.Result{
		ProviderReference: providerRef,
		Status:            provider.StatusPending,
	}, nil
}

// CheckStatus queries the settlement status of a bank transfer.
func (p *Provider) CheckStatus(ctx context.Context, providerReference string) (provider.Result, error) {
	p.mu.RLock()
	record, exists := p.transfers[providerReference]
	p.mu.RUnlock()

	if !exists {
		return provider.Result{}, fmt.Errorf("%w: %s", ErrTransferNotFound, providerReference)
	}

	return provider.Result{
		ProviderReference: record.ProviderReference,
		Status:            record.Status,
	}, nil
}

// SetStatus updates a transfer status (used by webhooks or mock settlements).
func (p *Provider) SetStatus(providerReference string, status provider.Status) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	record, exists := p.transfers[providerReference]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTransferNotFound, providerReference)
	}

	record.Status = status
	record.UpdatedAt = time.Now().UTC()
	return nil
}
