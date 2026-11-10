package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	provider "github.com/light-group/light-ex-backend/internal/modules/payment/provider"
	mtn "github.com/light-group/light-ex-backend/internal/modules/payment/provider/mtn"
	"github.com/light-group/light-ex-backend/internal/modules/payment/service"
	walletdomain "github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
)

type Handler struct {
	collector *service.Collector
}

// New creates a new payment initiation HTTP handler.
func New(collector *service.Collector) *Handler {
	return &Handler{
		collector: collector,
	}
}

// PaymentGate reports whether an authenticated user may move funds. It is
// implemented by the KYC module so payments do not depend on KYC internals.
type PaymentGate interface {
	PaymentAllowed(ctx context.Context, user uuid.UUID) (bool, error)
}

// RequireKYC aborts deposits and withdrawals unless the authenticated user
// passes the gate. Errors fail closed. It must run after authentication.
func RequireKYC(gate PaymentGate) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := uuid.Parse(middleware.UserID(c))
		if err != nil || userID == uuid.Nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized"})
			return
		}
		allowed, err := gate.PaymentAllowed(c.Request.Context(), userID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, ErrorResponse{Error: "kyc status unavailable"})
			return
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, ErrorResponse{Error: "KYC_VERIFICATION_REQUIRED"})
			return
		}
		c.Next()
	}
}

// RegisterDepositRoute mounts POST /payments/deposits on the group. The
// middleware run in order before the handler (authentication, then KYC gate).
func (h *Handler) RegisterDepositRoute(router *gin.RouterGroup, middleware ...gin.HandlerFunc) {
	router.POST("/payments/deposits", append(middleware, h.InitiateDeposit)...)
}

// RegisterWithdrawalRoute mounts POST /payments/withdrawals on the group. The
// middleware run in order before the handler (authentication, then KYC gate).
func (h *Handler) RegisterWithdrawalRoute(router *gin.RouterGroup, middleware ...gin.HandlerFunc) {
	router.POST("/payments/withdrawals", append(middleware, h.InitiateWithdrawal)...)
}

// InitiateDeposit starts a deposit for the authenticated user.
func (h *Handler) InitiateDeposit(c *gin.Context) {
	h.initiate(c, h.collector.InitiateDeposit)
}

// InitiateWithdrawal reserves the user's funds and starts a withdrawal.
func (h *Handler) InitiateWithdrawal(c *gin.Context) {
	h.initiate(c, h.collector.InitiateWithdrawal)
}

func (h *Handler) initiate(
	c *gin.Context,
	start func(ctx context.Context, input service.CollectionInput) (*service.CollectionResult, error),
) {
	userIDStr := middleware.UserID(c)
	if userIDStr == "" {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized"})
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid user identifier in token"})
		return
	}

	var req InitiatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	input, status := buildCollectionInput(userID, req)
	if status != "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: status})
		return
	}

	result, err := start(c.Request.Context(), input)
	if err != nil {
		respondToInitiationError(c, err)
		return
	}

	// A replayed already-completed transaction is reported back as-is; a live
	// initiation is answered with 202 Accepted because settlement arrives
	// later through the provider callback.
	if result.Payment.Status == paymentdomain.StatusProcessing {
		c.JSON(http.StatusAccepted, toPaymentResponse(result.Payment))
		return
	}

	c.JSON(http.StatusOK, toPaymentResponse(result.Payment))
}

func buildCollectionInput(userID uuid.UUID, req InitiatePaymentRequest) (service.CollectionInput, string) {
	walletID, err := uuid.Parse(strings.TrimSpace(req.WalletID))
	if err != nil {
		return service.CollectionInput{}, "wallet_id is required"
	}

	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil || !amount.GreaterThan(decimal.Zero) {
		return service.CollectionInput{}, "amount must be a positive decimal value"
	}

	currency := strings.TrimSpace(req.Currency)
	if l := len(currency); l < 3 || l > 10 {
		return service.CollectionInput{}, "currency must be between 3 and 10 characters"
	}

	if strings.TrimSpace(req.Phone) == "" {
		return service.CollectionInput{}, "phone is required"
	}

	return service.CollectionInput{
		DepositInput: service.DepositInput{
			UserID:      userID,
			WalletID:    walletID,
			Method:      mtn.ProviderName,
			Amount:      amount,
			Currency:    currency,
			Reference:   req.Reference,
			Description: req.Description,
		},
		Phone: req.Phone,
	}, ""
}

func respondToInitiationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrWalletNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "wallet not found"})
	case errors.Is(err, service.ErrWalletOwnerMismatch):
		c.JSON(http.StatusForbidden, ErrorResponse{Error: "access denied to this wallet"})
	case errors.Is(err, service.ErrReferenceConflict):
		c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
	case errors.Is(err, walletdomain.ErrInsufficientFunds):
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{Error: "insufficient funds"})
	case errors.Is(err, service.ErrLimitExceeded):
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{Error: err.Error()})
	case errors.Is(err, service.ErrDepositCurrencyMismatch),
		errors.Is(err, walletdomain.ErrWalletNotActive),
		errors.Is(err, mtn.ErrMissingCredentials),
		errors.Is(err, provider.ErrInvalidReference),
		errors.Is(err, provider.ErrInvalidAmount),
		errors.Is(err, provider.ErrInvalidCurrency),
		errors.Is(err, provider.ErrInvalidPhone):
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to initiate payment"})
	}
}
