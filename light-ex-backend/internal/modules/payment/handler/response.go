package handler

import "github.com/light-group/light-ex-backend/internal/modules/payment/domain"

type ErrorResponse struct {
	Error string `json:"error"`
}

// PaymentResponse is the summary returned for an initiated payment.
type PaymentResponse struct {
	ID                string `json:"id"`
	Reference         string `json:"reference"`
	Status            string `json:"status"`
	Amount            string `json:"amount"`
	Currency          string `json:"currency"`
	ProviderReference string `json:"provider_reference,omitempty"`
}

func toPaymentResponse(p *domain.Payment) PaymentResponse {
	return PaymentResponse{
		ID:                p.ID.String(),
		Reference:         p.Reference,
		Status:            string(p.Status),
		Amount:            p.Amount.StringFixed(8),
		Currency:          p.Currency,
		ProviderReference: p.ProviderReference,
	}
}
