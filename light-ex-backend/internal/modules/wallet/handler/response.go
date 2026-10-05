package handler

import (
	"time"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

type WalletResponse struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	AvailableBalance string    `json:"available_balance"`
	LockedBalance    string    `json:"locked_balance"`
	TotalBalance     string    `json:"total_balance"`
	Currency         string    `json:"currency"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func ToWalletResponse(w *domain.Wallet) WalletResponse {
	return WalletResponse{
		ID:               w.ID.String(),
		UserID:           w.UserID.String(),
		AvailableBalance: w.AvailableBalance.StringFixed(8),
		LockedBalance:    w.LockedBalance.StringFixed(8),
		TotalBalance:     w.TotalBalance().StringFixed(8),
		Currency:         w.Currency,
		Status:           string(w.Status),
		CreatedAt:        w.CreatedAt,
		UpdatedAt:        w.UpdatedAt,
	}
}
