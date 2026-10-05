package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/service"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
)

type Handler struct {
	service *service.Service
}

// New creates a new wallet HTTP handler.
func New(service *service.Service) *Handler {
	return &Handler{
		service: service,
	}
}

// RegisterRoutes registers all wallet routes on the provided router group.
func (h *Handler) RegisterRoutes(router *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	wallets := router.Group("/wallets")
	wallets.Use(authMiddleware)

	wallets.GET("/me", h.GetMyWallet)
	wallets.POST("", h.CreateWallet)
	wallets.GET("/:id", h.GetWalletByID)
}

// GetMyWallet retrieves the authenticated user's wallet.
func (h *Handler) GetMyWallet(c *gin.Context) {
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

	wallet, err := h.service.GetWalletByUserID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, service.ErrWalletNotFound) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "wallet not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to retrieve wallet"})
		return
	}

	c.JSON(http.StatusOK, ToWalletResponse(wallet))
}

// CreateWallet creates a new wallet for the authenticated user.
func (h *Handler) CreateWallet(c *gin.Context) {
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

	var req CreateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Non-fatal if body is empty or omitted; will default to USD
		req.Currency = "USD"
	}

	wallet, err := h.service.CreateWallet(c.Request.Context(), userID, req.Currency)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrWalletAlreadyExists):
			c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
		case errors.Is(err, service.ErrInvalidCurrency):
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		case errors.Is(err, service.ErrInvalidUserID):
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create wallet"})
		}
		return
	}

	c.JSON(http.StatusCreated, ToWalletResponse(wallet))
}

// GetWalletByID retrieves a wallet by its ID with ownership verification.
func (h *Handler) GetWalletByID(c *gin.Context) {
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

	idParam := c.Param("id")
	walletID, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid wallet id format"})
		return
	}

	wallet, err := h.service.GetWalletByID(c.Request.Context(), walletID)
	if err != nil {
		if errors.Is(err, service.ErrWalletNotFound) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "wallet not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to retrieve wallet"})
		return
	}

	// Verify the requesting user is the wallet owner
	if wallet.UserID != userID {
		c.JSON(http.StatusForbidden, ErrorResponse{Error: "access denied to this wallet"})
		return
	}

	c.JSON(http.StatusOK, ToWalletResponse(wallet))
}
