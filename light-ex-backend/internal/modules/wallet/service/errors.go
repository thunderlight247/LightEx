package service

import (
	"errors"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
)

var (
	ErrWalletNotFound          = errors.New("wallet not found")
	ErrWalletAlreadyExists     = errors.New("wallet already exists for this user")
	ErrWalletInactive          = errors.New("wallet is not active")
	ErrInsufficientFunds       = domain.ErrInsufficientFunds
	ErrInsufficientLockedFunds = domain.ErrInsufficientLockedFunds
	ErrInvalidAmount           = domain.ErrInvalidAmount
	ErrInvalidUserID           = domain.ErrInvalidUserID
	ErrInvalidCurrency         = domain.ErrInvalidCurrency
)
