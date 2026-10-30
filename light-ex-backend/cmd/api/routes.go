package main

import (
	"github.com/gin-gonic/gin"

	auth "github.com/light-group/light-ex-backend/internal/modules/auth"
	liquidity "github.com/light-group/light-ex-backend/internal/modules/liquidity"
	payment "github.com/light-group/light-ex-backend/internal/modules/payment"
	"github.com/light-group/light-ex-backend/internal/modules/payment/provider/mtn"
	wallet "github.com/light-group/light-ex-backend/internal/modules/wallet"
	"github.com/light-group/light-ex-backend/internal/platform/bootstrap"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
	jwtservice "github.com/light-group/light-ex-backend/internal/platform/security/jwt"
)

// RegisterRoutes registers all API routes.
func RegisterRoutes(
	router *gin.Engine,
	app *bootstrap.Application,
) {
	// API root
	api := router.Group("/api")

	// Version 1
	v1 := api.Group("/v1")

	// Health
	v1.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "LightEx API",
			"version": "v1",
		})
	})

	// Shared JWT service & middleware
	jwtService := jwtservice.New(app.Config.JWT)
	authMiddleware := middleware.Authentication(jwtService)

	// Authentication
	authModule := auth.New(
		app.Config,
		app.Database.SQL(),
	)
	authModule.RegisterRoutes(v1)

	// Wallet
	walletModule := wallet.New(app.Database.SQL())
	walletModule.RegisterRoutes(v1, authMiddleware)

	// Simulations are intentionally unavailable outside development.
	if app.Config.App.Env == "development" {
		liquidity.New().RegisterRoutes(v1, authMiddleware)
	}

	// MTN MoMo provider callbacks. Intentionally unauthenticated: caller
	// authenticity is established by SettlementProcessor, which validates the
	// callback against the stored payment and an authenticated MTN status
	// lookup before anything is written.
	// The route is mounted only when provider credentials are configured for at
	// least one product, so production never exposes it without a real provider.
	mtnCfg := mtn.LoadConfigFromEnv()
	if mtnCfg.Collection.IsConfigured() || mtnCfg.Disbursement.IsConfigured() {
		paymentModule := payment.New(app.Database.SQL())
		client := mtn.NewClient(mtnCfg)
		v1.POST("/payments/webhooks/mtn", mtn.WebhookHandler(
			mtn.SettlementProcessor(app.Database.SQL(), paymentModule.Repository(), client),
		))
	}
}
