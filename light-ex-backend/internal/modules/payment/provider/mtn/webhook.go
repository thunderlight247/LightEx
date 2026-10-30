package mtn

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	ledgerpostgres "github.com/light-group/light-ex-backend/internal/modules/ledger/repository/postgres"
	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
	paymentpostgres "github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
	paymentservice "github.com/light-group/light-ex-backend/internal/modules/payment/service"
	transactionpostgres "github.com/light-group/light-ex-backend/internal/modules/transaction/repository/postgres"
	walletpostgres "github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
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

// CallbackPaymentFinder looks up the local payment; the callback reference is only
// an untrusted lookup key, never proof of a payment's status.
type CallbackPaymentFinder interface {
	FindByReference(context.Context, string) (*paymentdomain.Payment, error)
}

type CallbackStatusLookup interface {
	LookupTransaction(context.Context, string, string) (TransactionStatusResponse, error)
}

var ErrCallbackVerification = errors.New("mtn callback verification failed")

// VerifiedCallback is a snapshot, not authorization to settle. A future settlement
// processor must re-lock and revalidate the payment in its transaction.
type VerifiedCallback struct {
	PaymentID         uuid.UUID
	Reference         string
	ProviderReference string
	Direction         paymentdomain.Direction
	Amount            decimal.Decimal
	Currency          string
	Status            provider.Status
}

// VerifyCallback verifies a callback against the saved payment and an authenticated
// MTN status lookup. It makes no writes and does not authenticate the webhook sender.
func VerifyCallback(ctx context.Context, finder CallbackPaymentFinder, lookup CallbackStatusLookup, n *CallbackNotification) (VerifiedCallback, error) {
	if finder == nil || lookup == nil || n == nil || strings.TrimSpace(n.ExternalID) == "" || n.ExternalID != strings.TrimSpace(n.ExternalID) {
		return VerifiedCallback{}, ErrCallbackVerification
	}
	p, err := finder.FindByReference(ctx, n.ExternalID)
	if err != nil {
		return VerifiedCallback{}, fmt.Errorf("find callback payment: %w", err)
	}
	if p == nil || p.ID == uuid.Nil || p.Reference != n.ExternalID || p.Method != ProviderName || p.Status != paymentdomain.StatusProcessing || !p.Amount.IsPositive() || p.Currency == "" {
		return VerifiedCallback{}, ErrCallbackVerification
	}
	id, err := uuid.Parse(p.ProviderReference)
	if err != nil || id == uuid.Nil || p.ProviderReference != id.String() {
		return VerifiedCallback{}, ErrCallbackVerification
	}
	if p.Direction != paymentdomain.DirectionDeposit && p.Direction != paymentdomain.DirectionWithdrawal {
		return VerifiedCallback{}, ErrCallbackVerification
	}
	callbackAmount, err := decimal.NewFromString(n.Amount)
	if err != nil || !callbackAmount.Equal(p.Amount) || n.Currency != p.Currency {
		return VerifiedCallback{}, ErrCallbackVerification
	}
	callbackStatus, err := strictCallbackStatus(n.Status)
	if err != nil {
		return VerifiedCallback{}, err
	}
	remote, err := lookup.LookupTransaction(ctx, string(p.Direction), p.ProviderReference)
	if err != nil {
		return VerifiedCallback{}, fmt.Errorf("verify mtn status: %w", err)
	}
	remoteAmount, err := decimal.NewFromString(remote.Amount)
	if err != nil || !remoteAmount.Equal(p.Amount) || remote.Currency != p.Currency || remote.ExternalID != p.Reference {
		return VerifiedCallback{}, ErrCallbackVerification
	}
	remoteStatus, err := strictCallbackStatus(remote.Status)
	if err != nil || remoteStatus != callbackStatus {
		return VerifiedCallback{}, ErrCallbackVerification
	}
	if n.FinancialTransactionID != "" && n.FinancialTransactionID != remote.FinancialTransactionID {
		return VerifiedCallback{}, ErrCallbackVerification
	}
	return VerifiedCallback{PaymentID: p.ID, Reference: p.Reference, ProviderReference: p.ProviderReference, Direction: p.Direction, Amount: p.Amount, Currency: p.Currency, Status: remoteStatus}, nil
}

func strictCallbackStatus(status string) (provider.Status, error) {
	switch status {
	case MTNStatusPending:
		return provider.StatusPending, nil
	case MTNStatusSuccessful:
		return provider.StatusSucceeded, nil
	case MTNStatusFailed:
		return provider.StatusFailed, nil
	default:
		return "", ErrCallbackVerification
	}
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
		normalized, hash, err := normalizedCallback(notif)
		if err != nil {
			return err
		}
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

// SettlementProcessor ties the full callback pipeline together:
//
//  1. parse (handled by WebhookHandler),
//  2. verify against the stored payment and authenticated MTN lookup (read-only,
//     strictly before any database write, so network latency never sits inside a
//     data transaction),
//  3. inside one transaction: deduplicate the event and apply the verified
//     settlement to payments, wallets, transactions and the ledger atomically.
//
// Settlement never trusts the callback body: it re-locks the payment row and
// revalidates every fact inside the transaction, so racing callbacks or the
// reconciliation worker settle each payment exactly once.
func SettlementProcessor(db *sql.DB, finder CallbackPaymentFinder, lookup CallbackStatusLookup) CallbackProcessor {
	return func(c *gin.Context, notif *CallbackNotification) error {
		ctx := c.Request.Context()

		// A dedup row with an identical payload hash exists only for callbacks that
		// already passed verification and whose settlement committed. Replaying one
		// must succeed without re-verifying the payment, which a later settlement
		// (or reconciliation pass) may have moved to a terminal status.
		normalized, hash, hashErr := normalizedCallback(notif)
		if hashErr == nil {
			var previous string
			err := db.QueryRowContext(ctx, `SELECT payload_hash FROM provider_callback_events WHERE provider = $1 AND reference = $2 AND status = $3 AND payload_hash = $4`, ProviderName, normalized.ExternalID, normalized.Status, hash).Scan(&previous)
			if err == nil {
				return nil // already verified and processed
			}
		}

		verified, err := VerifyCallback(ctx, finder, lookup, notif)
		if err != nil {
			return err
		}

		process := func(ctx context.Context, tx *sql.Tx, n *CallbackNotification) error {
			repos := paymentservice.DepositRepos{
				Payments:     paymentpostgres.New(tx),
				Transactions: transactionpostgres.New(tx),
				Wallets:      walletpostgres.New(tx),
				Ledger:       ledgerpostgres.New(tx),
			}
			return paymentservice.SettleCallback(ctx, repos, paymentservice.Settlement{
				Direction: verified.Direction,
				Reference: verified.Reference,
				Amount:    verified.Amount,
				Currency:  verified.Currency,
				Status:    verified.Status,
			})
		}
		return DeduplicatedProcessor(db, process)(c, notif)
	}
}

// normalizedCallback canonicalizes a notification for identity: trims the
// reference, upper-cases the status, and hashes the canonical JSON so JSON
// formatting does not change deduplication identity.
func normalizedCallback(n *CallbackNotification) (CallbackNotification, string, error) {
	norm := *n
	norm.ExternalID = strings.TrimSpace(n.ExternalID)
	norm.Status = strings.ToUpper(strings.TrimSpace(n.Status))
	payload, err := json.Marshal(norm)
	if err != nil {
		return norm, "", err
	}
	sum := sha256.Sum256(payload)
	return norm, hex.EncodeToString(sum[:]), nil
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
