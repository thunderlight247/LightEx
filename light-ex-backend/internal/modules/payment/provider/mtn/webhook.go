package mtn

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

var (
	ErrEmptyCallbackBody   = errors.New("mtn callback body is empty")
	ErrInvalidCallbackJSON = errors.New("mtn callback body is invalid json")
)

// CallbackNotification represents the JSON payload posted by MTN MoMo callback.
type CallbackNotification struct {
	FinancialTransactionID string `json:"financialTransactionId"`
	ExternalID             string `json:"externalId"`
	Amount                 string `json:"amount"`
	Currency               string `json:"currency"`
	Payer                  *Party `json:"payer,omitempty"`
	Payee                  *Party `json:"payee,omitempty"`
	Status                 string `json:"status"` // SUCCESSFUL, FAILED, PENDING
	Reason                 any    `json:"reason,omitempty"`
}

// Result converts the callback notification into a generic provider.Result.
func (c *CallbackNotification) Result() provider.Result {
	return provider.Result{
		ProviderReference: c.FinancialTransactionID,
		Status:            MapMTNStatus(c.Status),
	}
}

// ParseCallback deserializes raw callback request bytes into a CallbackNotification.
func ParseCallback(data []byte) (*CallbackNotification, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, ErrEmptyCallbackBody
	}

	var notif CallbackNotification
	if err := json.Unmarshal(data, &notif); err != nil {
		return nil, errors.Join(ErrInvalidCallbackJSON, err)
	}

	return &notif, nil
}

// CallbackProcessor is a callback hook that handles parsed MTN notifications.
type CallbackProcessor func(c *gin.Context, notif *CallbackNotification) error

// WebhookHandler returns a Gin handler function for MTN MoMo callback webhooks.
func WebhookHandler(processor CallbackProcessor) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read callback body"})
			return
		}

		notif, err := ParseCallback(body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if processor != nil {
			if err := processor(c, notif); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{"status": "received"})
	}
}
