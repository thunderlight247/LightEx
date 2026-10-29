package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransactionType string

const (
	TransactionTypeDeposit    TransactionType = "DEPOSIT"
	TransactionTypeWithdrawal TransactionType = "WITHDRAWAL"
	TransactionTypeTransfer   TransactionType = "TRANSFER"
	TransactionTypeExchange   TransactionType = "EXCHANGE"
	TransactionTypeAdjustment TransactionType = "ADJUSTMENT"
	TransactionTypeSettlement TransactionType = "SETTLEMENT"
)

type TransactionStatus string

const (
	TransactionStatusPending    TransactionStatus = "PENDING"
	TransactionStatusProcessing TransactionStatus = "PROCESSING"
	TransactionStatusCompleted  TransactionStatus = "COMPLETED"
	TransactionStatusFailed     TransactionStatus = "FAILED"
	TransactionStatusCancelled  TransactionStatus = "CANCELLED"
)

var (
	ErrInvalidWalletID          = errors.New("invalid wallet id")
	ErrInvalidTransactionType   = errors.New("invalid transaction type")
	ErrInvalidTransactionStatus = errors.New("invalid transaction status")
	ErrInvalidAmount            = errors.New("transaction amount must be greater than zero")
	ErrInvalidCurrency          = errors.New("invalid transaction currency")
	ErrInvalidReference         = errors.New("transaction reference cannot be empty")
	ErrInvalidStateTransition   = errors.New("invalid transaction state transition")
)

// Transaction represents a financial event affecting a wallet.
type Transaction struct {
	ID          uuid.UUID
	WalletID    uuid.UUID
	Type        TransactionType
	Amount      decimal.Decimal
	Currency    string
	Reference   string
	Status      TransactionStatus
	Description string
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GenerateReference creates a collision-resistant, human-traceable reference string.
func GenerateReference(prefix string) string {
	if prefix == "" {
		prefix = "TXN"
	}
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	timestamp := time.Now().UTC().Format("20060102150405")
	return fmt.Sprintf("%s-%s-%s", prefix, timestamp, hex.EncodeToString(bytes))
}

// IsValidType checks if a type string represents an allowed transaction type.
func IsValidType(t TransactionType) bool {
	switch t {
	case TransactionTypeDeposit,
		TransactionTypeWithdrawal,
		TransactionTypeTransfer,
		TransactionTypeExchange,
		TransactionTypeAdjustment,
		TransactionTypeSettlement:
		return true
	default:
		return false
	}
}

// NewTransaction creates a new transaction initialized in PENDING status.
func NewTransaction(
	walletID uuid.UUID,
	txType TransactionType,
	amount decimal.Decimal,
	currency string,
	reference string,
	description string,
	metadata map[string]any,
) (*Transaction, error) {
	if walletID == uuid.Nil {
		return nil, ErrInvalidWalletID
	}

	if !IsValidType(txType) {
		return nil, ErrInvalidTransactionType
	}

	if !amount.GreaterThan(decimal.Zero) {
		return nil, ErrInvalidAmount
	}

	currency = strings.TrimSpace(strings.ToUpper(currency))
	if len(currency) < 3 || len(currency) > 10 {
		return nil, ErrInvalidCurrency
	}

	reference = strings.TrimSpace(reference)
	if reference == "" {
		reference = GenerateReference(string(txType))
	}
	if len(reference) > 120 {
		return nil, errors.New("transaction reference exceeds 120 characters")
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}

	now := time.Now().UTC()

	return &Transaction{
		ID:          uuid.New(),
		WalletID:    walletID,
		Type:        txType,
		Amount:      amount,
		Currency:    currency,
		Reference:   reference,
		Status:      TransactionStatusPending,
		Description: strings.TrimSpace(description),
		Metadata:    metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// MarkProcessing moves a PENDING transaction to PROCESSING.
func (t *Transaction) MarkProcessing() error {
	if t.Status != TransactionStatusPending {
		return fmt.Errorf("%w: cannot transition from %s to PROCESSING", ErrInvalidStateTransition, t.Status)
	}

	t.Status = TransactionStatusProcessing
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkCompleted moves a PENDING or PROCESSING transaction to COMPLETED.
func (t *Transaction) MarkCompleted() error {
	if t.Status != TransactionStatusPending && t.Status != TransactionStatusProcessing {
		return fmt.Errorf("%w: cannot transition from %s to COMPLETED", ErrInvalidStateTransition, t.Status)
	}

	t.Status = TransactionStatusCompleted
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkFailed transitions a PENDING or PROCESSING transaction to FAILED.
func (t *Transaction) MarkFailed(reason string) error {
	if t.Status != TransactionStatusPending && t.Status != TransactionStatusProcessing {
		return fmt.Errorf("%w: cannot transition from %s to FAILED", ErrInvalidStateTransition, t.Status)
	}

	t.Status = TransactionStatusFailed
	if reason != "" {
		if t.Metadata == nil {
			t.Metadata = make(map[string]any)
		}
		t.Metadata["failure_reason"] = reason
	}
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkCancelled transitions a PENDING transaction to CANCELLED.
func (t *Transaction) MarkCancelled(reason string) error {
	if t.Status != TransactionStatusPending {
		return fmt.Errorf("%w: cannot transition from %s to CANCELLED", ErrInvalidStateTransition, t.Status)
	}

	t.Status = TransactionStatusCancelled
	if reason != "" {
		if t.Metadata == nil {
			t.Metadata = make(map[string]any)
		}
		t.Metadata["cancellation_reason"] = reason
	}
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// IsTerminal checks if the transaction is in a final, immutable state.
func (t *Transaction) IsTerminal() bool {
	return t.Status == TransactionStatusCompleted ||
		t.Status == TransactionStatusFailed ||
		t.Status == TransactionStatusCancelled
}
