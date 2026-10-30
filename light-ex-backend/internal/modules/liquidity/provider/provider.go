// Package provider defines liquidity provider contracts without settlement semantics.
package provider

import (
	"context"
	"errors"
	"time"

	"github.com/light-group/light-ex-backend/internal/modules/liquidity/domain"
	"github.com/shopspring/decimal"
)

var (
	ErrRejected  = errors.New("provider rejected request")
	ErrUncertain = errors.New("provider execution outcome is uncertain")
	ErrNotFound  = errors.New("provider order not found")
)

type Quote struct {
	Pair                 domain.Pair
	Amount, Rate, Output decimal.Decimal
	ExpiresAt            time.Time
	Provider             string
	Simulated            bool
}

type Result struct {
	Reference string
	Status    domain.Status
	Simulated bool
}

type Provider interface {
	Quote(context.Context, domain.Pair, decimal.Decimal) (Quote, error)
	// Execute must use reference as its idempotency key. A timeout or ambiguous error
	// must not be retried as a new order; check the same reference instead.
	Execute(context.Context, domain.Quote, string) (Result, error)
	Check(context.Context, string) (Result, error)
}
