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

type Direction string

const (
	DirectionDeposit    Direction = "DEPOSIT"
	DirectionWithdrawal Direction = "WITHDRAWAL"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
	StatusCancelled  Status = "CANCELLED"
)

var (
	ErrInvalidUserID          = errors.New("payment user id is required")
	ErrInvalidWalletID        = errors.New("payment wallet id is required")
	ErrInvalidDirection       = errors.New("invalid payment direction")
	ErrInvalidMethod          = errors.New("payment method is required")
	ErrInvalidAmount          = errors.New("payment amount must be greater than zero")
	ErrInvalidCurrency        = errors.New("payment currency is invalid")
	ErrInvalidReference       = errors.New("payment reference exceeds 120 characters")
	ErrInvalidStateTransition = errors.New("invalid payment state transition")
)

// Payment is the external or internal money-movement request that a deposit
// or withdrawal is driven by. It does not change balances itself.
type Payment struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	WalletID          uuid.UUID
	Direction         Direction
	Method            string
	Amount            decimal.Decimal
	Currency          string
	Status            Status
	Reference         string
	ProviderReference string
	Description       string
	Metadata          map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// GenerateReference creates a collision-resistant payment reference.
func GenerateReference(prefix string) string {
	if prefix == "" {
		prefix = "PAY"
	}
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	timestamp := time.Now().UTC().Format("20060102150405")
	return fmt.Sprintf("%s-%s-%s", prefix, timestamp, hex.EncodeToString(bytes))
}

// NewPayment validates input and creates a payment in PENDING status.
func NewPayment(
	userID uuid.UUID,
	walletID uuid.UUID,
	direction Direction,
	method string,
	amount decimal.Decimal,
	currency string,
	reference string,
	description string,
	metadata map[string]any,
) (*Payment, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidUserID
	}
	if walletID == uuid.Nil {
		return nil, ErrInvalidWalletID
	}
	if direction != DirectionDeposit && direction != DirectionWithdrawal {
		return nil, ErrInvalidDirection
	}

	method = strings.TrimSpace(strings.ToUpper(method))
	if method == "" || len(method) > 30 {
		return nil, ErrInvalidMethod
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
		reference = GenerateReference(string(direction))
	}
	if len(reference) > 120 {
		return nil, ErrInvalidReference
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}

	now := time.Now().UTC()

	return &Payment{
		ID:          uuid.New(),
		UserID:      userID,
		WalletID:    walletID,
		Direction:   direction,
		Method:      method,
		Amount:      amount,
		Currency:    currency,
		Status:      StatusPending,
		Reference:   reference,
		Description: strings.TrimSpace(description),
		Metadata:    metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// allowedTransitions defines the payment state machine. Terminal states
// (COMPLETED, FAILED, CANCELLED) have no outgoing transitions.
var allowedTransitions = map[Status][]Status{
	StatusPending:    {StatusProcessing, StatusFailed, StatusCancelled},
	StatusProcessing: {StatusCompleted, StatusFailed},
}

// CanTransition reports whether a payment may move from one status to another.
func CanTransition(from, to Status) bool {
	for _, next := range allowedTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// TransitionTo moves the payment to the target status if the state machine allows it.
func (p *Payment) TransitionTo(to Status) error {
	if !CanTransition(p.Status, to) {
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidStateTransition, p.Status, to)
	}
	p.Status = to
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// SetProviderReference records the external provider's reference for this payment.
func (p *Payment) SetProviderReference(ref string) {
	p.ProviderReference = strings.TrimSpace(ref)
	p.UpdatedAt = time.Now().UTC()
}

// IsTerminal reports whether the payment is in a final state.
func (p *Payment) IsTerminal() bool {
	return len(allowedTransitions[p.Status]) == 0
}
