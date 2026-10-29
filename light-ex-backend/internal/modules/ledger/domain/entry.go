package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type EntryType string

const (
	EntryTypeCredit EntryType = "CREDIT"
	EntryTypeDebit  EntryType = "DEBIT"
)

var (
	ErrInvalidTransactionID = errors.New("ledger entry transaction id is required")
	ErrInvalidWalletID      = errors.New("ledger entry wallet id is required")
	ErrInvalidEntryType     = errors.New("invalid ledger entry type")
	ErrInvalidAmount        = errors.New("ledger entry amount must be greater than zero")
	ErrInvalidCurrency      = errors.New("ledger entry currency is invalid")
)

// Entry is an immutable accounting record that a financial transaction posted against a wallet.
type Entry struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	WalletID      uuid.UUID
	EntryType     EntryType
	Amount        decimal.Decimal
	Currency      string
	CreatedAt     time.Time
}

// NewEntry validates and builds a ledger entry.
func NewEntry(
	transactionID uuid.UUID,
	walletID uuid.UUID,
	entryType EntryType,
	amount decimal.Decimal,
	currency string,
) (*Entry, error) {
	if transactionID == uuid.Nil {
		return nil, ErrInvalidTransactionID
	}
	if walletID == uuid.Nil {
		return nil, ErrInvalidWalletID
	}
	if entryType != EntryTypeCredit && entryType != EntryTypeDebit {
		return nil, ErrInvalidEntryType
	}
	if !amount.GreaterThan(decimal.Zero) {
		return nil, ErrInvalidAmount
	}

	currency = strings.TrimSpace(strings.ToUpper(currency))
	if len(currency) < 3 || len(currency) > 10 {
		return nil, ErrInvalidCurrency
	}

	return &Entry{
		ID:            uuid.New(),
		TransactionID: transactionID,
		WalletID:      walletID,
		EntryType:     entryType,
		Amount:        amount,
		Currency:      currency,
		CreatedAt:     time.Now().UTC(),
	}, nil
}
