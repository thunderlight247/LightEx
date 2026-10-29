package service

import (
	"errors"

	txdomain "github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	walletdomain "github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
)

var (
	ErrTransactionNotFound      = errors.New("transaction not found")
	ErrWalletNotFound           = errors.New("wallet not found")
	ErrCurrencyMismatch         = errors.New("transaction currency does not match wallet currency")
	ErrUnsupportedBalanceEffect = errors.New("transaction type has no supported balance effect")

	ErrInvalidStateTransition = txdomain.ErrInvalidStateTransition
	ErrInsufficientFunds      = walletdomain.ErrInsufficientFunds
	ErrWalletNotActive        = walletdomain.ErrWalletNotActive
)
