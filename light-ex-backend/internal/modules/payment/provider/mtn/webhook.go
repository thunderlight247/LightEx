package mtn

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

var (
	ErrEmptyCallbackBody    = errors.New("mtn callback body is empty")
	ErrInvalidCallbackJSON  = errors.New("mtn callback body is invalid json")
	ErrInvalidCallbackEvent = errors.New("mtn callback requires a valid external reference and status")
	ErrCallbackConflict     = errors.New("mtn callback conflicts with an already processed event")
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

// TransactionalCallbackProcessor must apply all database effects using tx. It must
// not perform irreversible external side effects: the database transaction can roll back.
type TransactionalCallbackProcessor func(ctx context.Context, tx *sql.Tx, notif *CallbackNotification) error

// DeduplicatedProcessor runs a verified callback and its database effects atomically.
// The caller must authenticate the callback or independently verify its provider status
// before calling this processor; deduplication does not establish authenticity.
func DeduplicatedProcessor(db *sql.DB, process TransactionalCallbackProcessor) CallbackProcessor {
	return func(c *gin.Context, notif *CallbackNotification) error {
		if db == nil || process == nil || notif == nil {
			return errors.New("mtn callback processor is not configured")
		}
		ref := strings.TrimSpace(notif.ExternalID)
		status := strings.ToUpper(strings.TrimSpace(notif.Status))
		if ref == "" || len(ref) > 120 || (status != MTNStatusPending && status != MTNStatusSuccessful && status != MTNStatusFailed) {
			return ErrInvalidCallbackEvent
		}
		// Hash a canonical JSON representation so JSON formatting does not change identity.
		normalized := *notif
		normalized.ExternalID, normalized.Status = ref, status
		payload, err := json.Marshal(normalized)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(payload)
		hash := hex.EncodeToString(sum[:])
		ctx := c.Request.Context()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(ctx, `INSERT INTO provider_callback_events (provider, reference, status, payload_hash)
VALUES ($1, $2, $3, $4) ON CONFLICT (provider, reference, status) DO NOTHING`, ProviderName, ref, status, hash)
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted == 0 {
			var previous string
			if err := tx.QueryRowContext(ctx, `SELECT payload_hash FROM provider_callback_events WHERE provider = $1 AND reference = $2 AND status = $3`, ProviderName, ref, status).Scan(&previous); err != nil {
				return err
			}
			if previous != hash {
				return ErrCallbackConflict
			}
			return tx.Commit()
		}
		if err := process(ctx, tx, &normalized); err != nil {
			return err
		}
		return tx.Commit()
	}
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
