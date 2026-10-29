package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func newTestPayment(t *testing.T) *Payment {
	t.Helper()
	p, err := NewPayment(uuid.New(), uuid.New(), DirectionDeposit, "mobile_money", decimal.NewFromInt(25), " usd ", "", "test", nil)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	return p
}

func TestNewPayment_Valid(t *testing.T) {
	p := newTestPayment(t)
	if p.Status != StatusPending {
		t.Fatalf("expected PENDING, got %s", p.Status)
	}
	if p.Currency != "USD" || p.Method != "MOBILE_MONEY" {
		t.Fatalf("expected normalized currency/method, got %s/%s", p.Currency, p.Method)
	}
	if p.Reference == "" || p.Metadata == nil {
		t.Fatalf("expected generated reference and metadata map: %+v", p)
	}
}

func TestNewPayment_Validation(t *testing.T) {
	validUser := uuid.New()
	validWallet := uuid.New()
	ten := decimal.NewFromInt(10)
	long := make([]byte, 121)
	for i := range long {
		long[i] = 'a'
	}

	tests := []struct {
		name      string
		user      uuid.UUID
		wallet    uuid.UUID
		direction Direction
		method    string
		amount    decimal.Decimal
		currency  string
		reference string
		wantErr   error
	}{
		{"nil user", uuid.Nil, validWallet, DirectionDeposit, "CARD", ten, "USD", "", ErrInvalidUserID},
		{"nil wallet", validUser, uuid.Nil, DirectionDeposit, "CARD", ten, "USD", "", ErrInvalidWalletID},
		{"bad direction", validUser, validWallet, Direction("TRANSFER"), "CARD", ten, "USD", "", ErrInvalidDirection},
		{"empty method", validUser, validWallet, DirectionDeposit, "  ", ten, "USD", "", ErrInvalidMethod},
		{"zero amount", validUser, validWallet, DirectionDeposit, "CARD", decimal.Zero, "USD", "", ErrInvalidAmount},
		{"short currency", validUser, validWallet, DirectionDeposit, "CARD", ten, "US", "", ErrInvalidCurrency},
		{"long reference", validUser, validWallet, DirectionDeposit, "CARD", ten, "USD", string(long), ErrInvalidReference},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPayment(tt.user, tt.wallet, tt.direction, tt.method, tt.amount, tt.currency, tt.reference, "", nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestPaymentStateMachine_AllowedTransitions(t *testing.T) {
	p := newTestPayment(t)

	if err := p.TransitionTo(StatusProcessing); err != nil {
		t.Fatalf("PENDING -> PROCESSING: %v", err)
	}
	if err := p.TransitionTo(StatusCompleted); err != nil {
		t.Fatalf("PROCESSING -> COMPLETED: %v", err)
	}
	if !p.IsTerminal() {
		t.Fatalf("COMPLETED should be terminal")
	}
}

func TestPaymentStateMachine_RejectedTransitions(t *testing.T) {
	tests := []struct {
		from Status
		to   Status
	}{
		{StatusPending, StatusCompleted},  // must pass through PROCESSING
		{StatusCompleted, StatusFailed},   // terminal
		{StatusFailed, StatusProcessing},  // terminal
		{StatusCancelled, StatusPending},  // terminal
		{StatusProcessing, StatusPending}, // no way back
		{StatusProcessing, StatusCancelled},
	}

	for _, tt := range tests {
		if CanTransition(tt.from, tt.to) {
			t.Fatalf("%s -> %s should be rejected", tt.from, tt.to)
		}
	}
}

func TestPaymentStateMachine_TransitionErrorIsReturned(t *testing.T) {
	p := newTestPayment(t)
	err := p.TransitionTo(StatusCompleted)
	if !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
	}
	if p.Status != StatusPending {
		t.Fatalf("status must not change on a rejected transition, got %s", p.Status)
	}
}

func TestPaymentIsTerminal(t *testing.T) {
	p := newTestPayment(t)
	if p.IsTerminal() {
		t.Fatalf("PENDING should not be terminal")
	}
	if err := p.TransitionTo(StatusFailed); err != nil {
		t.Fatalf("PENDING -> FAILED: %v", err)
	}
	if !p.IsTerminal() {
		t.Fatalf("FAILED should be terminal")
	}
}
