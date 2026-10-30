package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/domain"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/service"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
	"github.com/shopspring/decimal"
)

type Handler struct{ service *service.Service }

func New(s *service.Service) *Handler { return &Handler{service: s} }
func (h *Handler) RegisterRoutes(r *gin.RouterGroup, auth gin.HandlerFunc) {
	g := r.Group("/simulated-liquidity")
	g.Use(auth)
	g.POST("/quotes", h.quote)
	g.POST("/orders", h.order)
	g.GET("/orders/:id", h.getOrder)
	g.POST("/orders/:id/reconcile", h.reconcile)
}
func user(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(middleware.UserID(c))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return uuid.Nil, false
	}
	return id, true
}
func respondError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrInvalidPair), errors.Is(err, domain.ErrInvalidAmount), errors.Is(err, service.ErrInvalidKey):
		status = http.StatusBadRequest
	case errors.Is(err, service.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, service.ErrConflict), errors.Is(err, domain.ErrQuoteExpired):
		status = http.StatusConflict
	case errors.Is(err, provider.ErrRejected):
		status = http.StatusUnprocessableEntity
	}
	if status == http.StatusInternalServerError {
		c.JSON(status, gin.H{"error": "simulation could not be processed"})
		return
	}
	c.JSON(status, gin.H{"error": err.Error()})
}
func (h *Handler) quote(c *gin.Context) {
	id, ok := user(c)
	if !ok {
		return
	}
	var req struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount string `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid amount"})
		return
	}
	q, err := h.service.Quote(c.Request.Context(), id, domain.Pair{From: req.From, To: req.To}, amount)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, q)
}
func (h *Handler) order(c *gin.Context) {
	id, ok := user(c)
	if !ok {
		return
	}
	var req struct {
		QuoteID        uuid.UUID `json:"quote_id"`
		IdempotencyKey string    `json:"idempotency_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	o, err := h.service.CreateOrder(c.Request.Context(), id, req.QuoteID, req.IdempotencyKey)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}
func (h *Handler) getOrder(c *gin.Context) {
	userID, ok := user(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid order id"})
		return
	}
	o, err := h.service.GetOrder(userID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(200, o)
}
func (h *Handler) reconcile(c *gin.Context) {
	userID, ok := user(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid order id"})
		return
	}
	o, err := h.service.Reconcile(c.Request.Context(), userID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(200, o)
}
