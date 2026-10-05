package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type WalletStatus string

const (
	WalletStatusActive    WalletStatus = "ACTIVE"
	WalletStatusSuspended WalletStatus = "SUSPENDED"
	WalletStatusFrozen    WalletStatus = "FROZEN"
	WalletStatusClosed    WalletStatus = "CLOSED"
)

var (
	ErrInvalidUserID           = errors.New("invalid user id")
	ErrInvalidCurrency         = errors.New("invalid currency")
	ErrInvalidAmount           = errors.New("amount must be greater than zero")
	ErrInsufficientFunds       = errors.New("insufficient available balance")
	ErrInsufficientLockedFunds = errors.New("insufficient locked balance")
	ErrWalletNotActive         = errors.New("wallet is not active")
)

// Wallet represents a user's monetary balance container.
// It maintains both available and locked funds with precise decimal arithmetic.
type Wallet struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	AvailableBalance decimal.Decimal
	LockedBalance    decimal.Decimal
	Currency         string
	Status           WalletStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NewWallet creates a new active wallet for a user with zero balance.
func NewWallet(userID uuid.UUID, currency string) (*Wallet, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidUserID
	}

	currency = strings.TrimSpace(strings.ToUpper(currency))
	if currency == "" {
		currency = "USD"
	}

	if len(currency) < 3 || len(currency) > 10 {
		return nil, ErrInvalidCurrency
	}

	now := time.Now().UTC()

	return &Wallet{
		ID:               uuid.New(),
		UserID:           userID,
		AvailableBalance: decimal.Zero,
		LockedBalance:    decimal.Zero,
		Currency:         currency,
		Status:           WalletStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

// TotalBalance returns the sum of available and locked balances.
func (w *Wallet) TotalBalance() decimal.Decimal {
	return w.AvailableBalance.Add(w.LockedBalance)
}

// IsActive reports whether the wallet is in ACTIVE status.
func (w *Wallet) IsActive() bool {
	return w.Status == WalletStatusActive
}

// Credit adds funds to the available balance.
func (w *Wallet) Credit(amount decimal.Decimal) error {
	if !w.IsActive() {
		return ErrWalletNotActive
	}

	if !amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}

	w.AvailableBalance = w.AvailableBalance.Add(amount)
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// Debit removes funds from the available balance.
func (w *Wallet) Debit(amount decimal.Decimal) error {
	if !w.IsActive() {
		return ErrWalletNotActive
	}

	if !amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}

	if w.AvailableBalance.LessThan(amount) {
		return ErrInsufficientFunds
	}

	w.AvailableBalance = w.AvailableBalance.Sub(amount)
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// LockFunds moves funds from available balance to locked balance.
// Used when an operation (like a pending withdrawal or exchange order) reserves funds.
func (w *Wallet) LockFunds(amount decimal.Decimal) error {
	if !w.IsActive() {
		return ErrWalletNotActive
	}

	if !amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}

	if w.AvailableBalance.LessThan(amount) {
		return ErrInsufficientFunds
	}

	w.AvailableBalance = w.AvailableBalance.Sub(amount)
	w.LockedBalance = w.LockedBalance.Add(amount)
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// UnlockFunds moves funds from locked balance back to available balance.
// Used when a reserved operation is cancelled or rejected.
func (w *Wallet) UnlockFunds(amount decimal.Decimal) error {
	if !w.IsActive() {
		return ErrWalletNotActive
	}

	if !amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}

	if w.LockedBalance.LessThan(amount) {
		return ErrInsufficientLockedFunds
	}

	w.LockedBalance = w.LockedBalance.Sub(amount)
	w.AvailableBalance = w.AvailableBalance.Add(amount)
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// DebitLocked removes funds directly from the locked balance.
// Used when a pending operation that previously locked funds is finalized/settled.
func (w *Wallet) DebitLocked(amount decimal.Decimal) error {
	if !amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}

	if w.LockedBalance.LessThan(amount) {
		return ErrInsufficientLockedFunds
	}

	w.LockedBalance = w.LockedBalance.Sub(amount)
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// Activate sets wallet status to ACTIVE.
func (w *Wallet) Activate() {
	w.Status = WalletStatusActive
	w.UpdatedAt = time.Now().UTC()
}

// Suspend sets wallet status to SUSPENDED.
func (w *Wallet) Suspend() {
	w.Status = WalletStatusSuspended
	w.UpdatedAt = time.Now().UTC()
}

// Freeze sets wallet status to FROZEN.
func (w *Wallet) Freeze() {
	w.Status = WalletStatusFrozen
	w.UpdatedAt = time.Now().UTC()
}

// Close sets wallet status to CLOSED.
func (w *Wallet) Close() {
	w.Status = WalletStatusClosed
	w.UpdatedAt = time.Now().UTC()
}
