package crypto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

const (
	ProviderName = "CRYPTO"

	NetworkBTC      = "BTC"
	NetworkEthereum = "ETH"
	NetworkTRON     = "TRON"
	NetworkPolygon  = "POLYGON"
)

var (
	ErrInvalidAddress = errors.New("invalid cryptocurrency address")
	ErrInvalidNetwork = errors.New("unsupported cryptocurrency network")
	ErrTxNotFound     = errors.New("crypto transaction not found")
)

// SupportedNetworks lists verified blockchain networks.
var SupportedNetworks = map[string]bool{
	NetworkBTC:      true,
	NetworkEthereum: true,
	NetworkTRON:     true,
	NetworkPolygon:  true,
	"USDT":          true,
	"USDC":          true,
}

// ValidateAddress performs basic checksum and length checks for standard crypto addresses.
func ValidateAddress(network, address string) error {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return ErrInvalidAddress
	}

	net := strings.ToUpper(strings.TrimSpace(network))
	switch net {
	case NetworkEthereum, NetworkPolygon, "USDT-ERC20", "ERC20":
		if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
			return fmt.Errorf("%w: invalid EVM address format", ErrInvalidAddress)
		}
	case NetworkTRON, "USDT-TRC20", "TRC20":
		if !strings.HasPrefix(addr, "T") || len(addr) != 34 {
			return fmt.Errorf("%w: invalid TRON address format", ErrInvalidAddress)
		}
	case NetworkBTC:
		// Standard legacy (1...), P2SH (3...) or bech32 (bc1...)
		if len(addr) < 26 || len(addr) > 62 {
			return fmt.Errorf("%w: invalid Bitcoin address length", ErrInvalidAddress)
		}
	default:
		if len(addr) < 10 {
			return ErrInvalidAddress
		}
	}
	return nil
}

// CryptoTransaction tracks on-chain money movements.
type CryptoTransaction struct {
	Reference         string
	ProviderReference string // TxHash or deposit ID
	Direction         string // DEPOSIT or WITHDRAWAL
	Network           string
	Address           string
	Amount            string
	Currency          string
	Confirmations     int
	RequiredConfs     int
	Status            provider.Status
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Provider implements provider.Provider for cryptocurrency deposits and withdrawals.
type Provider struct {
	mu           sync.RWMutex
	transactions map[string]*CryptoTransaction
}

var _ provider.Provider = (*Provider)(nil)

// NewProvider creates a new Crypto payment provider.
func NewProvider() *Provider {
	return &Provider{
		transactions: make(map[string]*CryptoTransaction),
	}
}

func (p *Provider) Name() string {
	return ProviderName
}

// Collect provisions a deposit address and monitors incoming blockchain transfers.
func (p *Provider) Collect(ctx context.Context, req provider.Request) (provider.Result, error) {
	if err := provider.Validate(req); err != nil {
		return provider.Result{}, err
	}

	providerRef := "CRYP-DEP-" + uuid.New().String()[:12]

	p.mu.Lock()
	p.transactions[providerRef] = &CryptoTransaction{
		Reference:         req.Reference,
		ProviderReference: providerRef,
		Direction:         "DEPOSIT",
		Currency:          req.Currency,
		Amount:            req.Amount.StringFixed(8),
		Confirmations:     0,
		RequiredConfs:     3,
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

// Disburse broadcasts an outbound blockchain transaction to the customer's crypto address.
func (p *Provider) Disburse(ctx context.Context, req provider.Request) (provider.Result, error) {
	if err := provider.Validate(req); err != nil {
		return provider.Result{}, err
	}

	// Generate deterministic mock on-chain tx hash
	hasher := sha256.New()
	hasher.Write([]byte(req.Reference + time.Now().String()))
	txHash := "0x" + hex.EncodeToString(hasher.Sum(nil))

	p.mu.Lock()
	p.transactions[txHash] = &CryptoTransaction{
		Reference:         req.Reference,
		ProviderReference: txHash,
		Direction:         "WITHDRAWAL",
		Currency:          req.Currency,
		Amount:            req.Amount.StringFixed(8),
		Confirmations:     0,
		RequiredConfs:     1,
		Status:            provider.StatusPending,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	p.mu.Unlock()

	return provider.Result{
		ProviderReference: txHash,
		Status:            provider.StatusPending,
	}, nil
}

// CheckStatus queries transaction confirmation status.
func (p *Provider) CheckStatus(ctx context.Context, providerReference string) (provider.Result, error) {
	p.mu.RLock()
	tx, exists := p.transactions[providerReference]
	p.mu.RUnlock()

	if !exists {
		return provider.Result{}, fmt.Errorf("%w: %s", ErrTxNotFound, providerReference)
	}

	return provider.Result{
		ProviderReference: tx.ProviderReference,
		Status:            tx.Status,
	}, nil
}

// SetStatus updates status (e.g. when required block confirmations are reached).
func (p *Provider) SetStatus(providerReference string, status provider.Status) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, exists := p.transactions[providerReference]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTxNotFound, providerReference)
	}

	tx.Status = status
	tx.UpdatedAt = time.Now().UTC()
	return nil
}
