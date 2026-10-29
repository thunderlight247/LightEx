// Package provider defines the contract that external payment networks implement.
// Deposits are collected from a customer's mobile wallet and withdrawals are
// disbursed to it. Provider implementations live outside this package.
package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/shopspring/decimal"
)

var (
	ErrInvalidReference = errors.New("provider request reference is required")
	ErrInvalidAmount    = errors.New("provider request amount must be greater than zero")
	ErrInvalidCurrency  = errors.New("provider request currency is invalid")
	ErrInvalidPhone     = errors.New("provider request phone number is required")
)

// Status is the provider's view of an external money movement.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
)

// Request describes one external money movement. Reference is the LightEx
// payment reference and is sent to the provider so callbacks can be matched.
type Request struct {
	Reference string
	Amount    decimal.Decimal
	Currency  string
	Phone     string
}

// Result is the provider's answer to a request.
type Result struct {
	ProviderReference string
	Status            Status
}

// Provider is implemented by each external payment network.
type Provider interface {
	// Name identifies the provider, for example "MOBILE_MONEY".
	Name() string
	// Collect starts a deposit: it asks the customer to pay into LightEx.
	Collect(ctx context.Context, req Request) (Result, error)
	// Disburse starts a withdrawal: it sends money from LightEx to the customer.
	Disburse(ctx context.Context, req Request) (Result, error)
}

// Validate checks a request before it is sent to any provider.
func Validate(req Request) error {
	if strings.TrimSpace(req.Reference) == "" {
		return ErrInvalidReference
	}
	if !req.Amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}
	currency := strings.TrimSpace(req.Currency)
	if len(currency) < 3 || len(currency) > 10 {
		return ErrInvalidCurrency
	}
	if strings.TrimSpace(req.Phone) == "" {
		return ErrInvalidPhone
	}
	return nil
}
