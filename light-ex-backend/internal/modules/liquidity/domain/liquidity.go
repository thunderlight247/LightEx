// Package domain contains liquidity models; it has no custody or settlement behavior.
package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrInvalidPair   = errors.New("invalid asset pair")
	ErrInvalidAmount = errors.New("amount must be greater than zero")
	ErrInvalidQuote  = errors.New("invalid provider quote")
	ErrQuoteExpired  = errors.New("quote expired")
	ErrTransition    = errors.New("invalid order state transition")
)

type Pair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func NewPair(from, to string) (Pair, error) {
	p := Pair{strings.ToUpper(strings.TrimSpace(from)), strings.ToUpper(strings.TrimSpace(to))}
	valid := func(s string) bool {
		if len(s) < 3 || len(s) > 10 {
			return false
		}
		for _, r := range s {
			if r < 'A' || r > 'Z' {
				return false
			}
		}
		return true
	}
	if !valid(p.From) || !valid(p.To) || p.From == p.To {
		return Pair{}, ErrInvalidPair
	}
	return p, nil
}

type Quote struct {
	ID        uuid.UUID       `json:"id"`
	UserID    uuid.UUID       `json:"-"`
	Pair      Pair            `json:"pair"`
	Amount    decimal.Decimal `json:"amount"`
	Rate      decimal.Decimal `json:"rate"`
	Output    decimal.Decimal `json:"output"`
	ExpiresAt time.Time       `json:"expires_at"`
	Provider  string          `json:"provider"`
	Simulated bool            `json:"simulated"`
}

func (q Quote) Validate(now time.Time) error {
	if q.ID == uuid.Nil || q.UserID == uuid.Nil || q.Provider == "" {
		return ErrInvalidQuote
	}
	pair, err := NewPair(q.Pair.From, q.Pair.To)
	if err != nil || pair != q.Pair || !q.Amount.IsPositive() || !q.Rate.IsPositive() || !q.Output.IsPositive() || !q.Output.Equal(q.Amount.Mul(q.Rate)) {
		return ErrInvalidQuote
	}
	if !now.Before(q.ExpiresAt) {
		return ErrQuoteExpired
	}
	return nil
}

type Status string

const (
	StatusPending   Status = "PENDING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
	StatusUncertain Status = "UNCERTAIN"
)

// Status is provider-reported only. No status indicates verified settlement.
type Order struct {
	ID                uuid.UUID `json:"id"`
	UserID            uuid.UUID `json:"-"`
	QuoteID           uuid.UUID `json:"quote_id"`
	IdempotencyKey    string    `json:"idempotency_key"`
	ProviderReference string    `json:"provider_reference"`
	Status            Status    `json:"provider_status"`
	Settlement        string    `json:"settlement"`
	Simulated         bool      `json:"simulated"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (o *Order) Transition(next Status, now time.Time) error {
	if next == o.Status {
		return nil
	}
	if (o.Status != StatusPending && o.Status != StatusUncertain) || (next != StatusPending && next != StatusUncertain && next != StatusSucceeded && next != StatusFailed) {
		return ErrTransition
	}
	o.Status, o.UpdatedAt = next, now.UTC()
	return nil
}
