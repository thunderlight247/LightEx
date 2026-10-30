package handler

// InitiatePaymentRequest is the body of a deposit or withdrawal initiation.
// Amount is a decimal string so precision is not lost to float parsing on
// money fields.
type InitiatePaymentRequest struct {
	WalletID    string `json:"wallet_id"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Phone       string `json:"phone"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
}
