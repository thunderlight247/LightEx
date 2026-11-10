package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/domain"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/service"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
)

type Handler struct{ service *service.Service }

func New(s *service.Service) *Handler { return &Handler{service: s} }
func (h *Handler) RegisterRoutes(router *gin.RouterGroup, auth gin.HandlerFunc) {
	g := router.Group("/kyc")
	g.Use(auth)
	g.POST("/submissions", h.submit)
	g.GET("/me", h.me)
	g.GET("/eligibility", h.eligibility)
	g.GET("/submissions/:id", h.requireAccessToken, h.getForReview)
	g.POST("/submissions/:id/review", h.requireAccessToken, h.review)
}
func userID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(middleware.UserID(c))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return uuid.Nil, false
	}
	return id, true
}
func respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidSubmission), errors.Is(err, domain.ErrInvalidReview):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "reviewer access denied"})
	case errors.Is(err, repository.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, repository.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "KYC submission not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "KYC request failed"})
	}
}
func (h *Handler) submit(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		return
	}
	var req struct {
		LegalName   string `json:"legal_name"`
		DateOfBirth string `json:"date_of_birth"`
		Country     string `json:"country"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	submission, err := h.service.Submit(c.Request.Context(), id, req.LegalName, req.DateOfBirth, req.Country)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, submission)
}
func (h *Handler) requireAccessToken(c *gin.Context) {
	if tokenType, ok := c.Get(middleware.ContextTokenType); !ok || tokenType != "access" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "access token required"})
		return
	}
	c.Next()
}

func (h *Handler) getForReview(c *gin.Context) {
	reviewer, ok := userID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid submission id"})
		return
	}
	submission, err := h.service.GetForReview(c.Request.Context(), id, reviewer)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"submission": submission, "user_id": submission.UserID})
}
func (h *Handler) review(c *gin.Context) {
	reviewer, ok := userID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid submission id"})
		return
	}
	var req struct {
		Status domain.Status `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	submission, err := h.service.Review(c.Request.Context(), id, reviewer, req.Status)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, submission)
}

func (h *Handler) eligibility(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		return
	}
	assessment, err := h.service.Eligibility(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, assessment)
}

func (h *Handler) me(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		return
	}
	submission, err := h.service.Latest(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, submission)
}
