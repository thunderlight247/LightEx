package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewEntry_Valid(t *testing.T) {
	e, err := NewEntry(uuid.New(), uuid.New(), EntryTypeCredit, decimal.NewFromInt(10), " usd ")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	if e.Currency != "USD" {
		t.Fatalf("expected normalized currency USD, got %s", e.Currency)
	}
	if e.ID == uuid.Nil || e.CreatedAt.IsZero() {
		t.Fatalf("expected ID and CreatedAt to be set: %+v", e)
	}
}

func TestEntryTypeForTransaction(t *testing.T) {
	cases := map[string]EntryType{
		"DEPOSIT":    EntryTypeCredit,
		"WITHDRAWAL": EntryTypeDebit,
	}
	for txType, want := range cases {
		got, err := EntryTypeForTransaction(txType)
		if err != nil || got != want {
			t.Fatalf("%s: want %s, got %s (err %v)", txType, want, got, err)
		}
	}

	for _, txType := range []string{"TRANSFER", "EXCHANGE", "ADJUSTMENT", ""} {
		if _, err := EntryTypeForTransaction(txType); !errors.Is(err, ErrNoEntryForTransaction) {
			t.Fatalf("%q: expected ErrNoEntryForTransaction, got %v", txType, err)
		}
	}
}

func TestNewEntry_Validation(t *testing.T) {
	validTx := uuid.New()
	validWallet := uuid.New()
	ten := decimal.NewFromInt(10)

	tests := []struct {
		name      string
		tx        uuid.UUID
		wallet    uuid.UUID
		entryType EntryType
		amount    decimal.Decimal
		currency  string
		wantErr   error
	}{
		{"nil transaction", uuid.Nil, validWallet, EntryTypeCredit, ten, "USD", ErrInvalidTransactionID},
		{"nil wallet", validTx, uuid.Nil, EntryTypeCredit, ten, "USD", ErrInvalidWalletID},
		{"bad type", validTx, validWallet, EntryType("OTHER"), ten, "USD", ErrInvalidEntryType},
		{"zero amount", validTx, validWallet, EntryTypeDebit, decimal.Zero, "USD", ErrInvalidAmount},
		{"negative amount", validTx, validWallet, EntryTypeDebit, decimal.NewFromInt(-1), "USD", ErrInvalidAmount},
		{"short currency", validTx, validWallet, EntryTypeCredit, ten, "US", ErrInvalidCurrency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEntry(tt.tx, tt.wallet, tt.entryType, tt.amount, tt.currency)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}
