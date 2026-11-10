package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	auth "github.com/light-group/light-ex-backend/internal/modules/auth"
	kyc "github.com/light-group/light-ex-backend/internal/modules/kyc"
	liquidity "github.com/light-group/light-ex-backend/internal/modules/liquidity"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider/simulated"
	payment "github.com/light-group/light-ex-backend/internal/modules/payment"
	"github.com/light-group/light-ex-backend/internal/modules/payment/provider/mtn"
	"github.com/light-group/light-ex-backend/internal/modules/payment/service"
	wallet "github.com/light-group/light-ex-backend/internal/modules/wallet"
	"github.com/light-group/light-ex-backend/internal/platform/bootstrap"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
	jwtservice "github.com/light-group/light-ex-backend/internal/platform/security/jwt"
)

// paymentLimitsFromEnv reads optional per-transaction maximums. An unset value
// means unlimited. A set value that is not a non-negative decimal stops startup,
// so a typo can never silently remove a configured cap.
func paymentLimitsFromEnv() service.Limits {
	return service.Limits{
		MaxDeposit:    mustLimitFromEnv("PAYMENT_MAX_DEPOSIT"),
		MaxWithdrawal: mustLimitFromEnv("PAYMENT_MAX_WITHDRAWAL"),
	}
}

func mustLimitFromEnv(key string) decimal.Decimal {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return decimal.Zero
	}
	value, err := decimal.NewFromString(raw)
	if err != nil || value.IsNegative() {
		panic(fmt.Sprintf("%s must be a non-negative decimal, got %q", key, raw))
	}
	return value
}

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

	// KYC submissions and status are scoped to the authenticated user.
	kycModule := kyc.New(app.Database.SQL())
	kycModule.RegisterRoutes(v1, authMiddleware)

	// Simulations are intentionally unavailable outside development.
	if app.Config.App.Env == "development" {
		liquidity.New(simulated.Scenario(app.Config.App.SimulatedLiquidityScenario)).RegisterRoutes(v1, authMiddleware)
	}

	// MTN MoMo provider. The callback route is guarded by a shared secret path
	// segment, and SettlementProcessor also validates each callback against the
	// stored payment and an authenticated MTN status lookup before anything is written. Initiation routes require the standard
	// JWT auth middleware. Routes are mounted only when provider credentials are
	// configured for the corresponding product, so production never exposes an
	// endpoint it cannot service.
	mtnCfg := mtn.LoadConfigFromEnv()
	if mtnCfg.Collection.IsConfigured() || mtnCfg.Disbursement.IsConfigured() {
		client := mtn.NewClient(mtnCfg)

		// Deposits and withdrawals require an APPROVED KYC submission.
		paymentModule := payment.New(app.Database.SQL()).
			WithLimits(paymentLimitsFromEnv()).
			WithInitiator(client).
			WithKYCGate(kycModule.PaymentGate())

		if mtnCfg.Collection.IsConfigured() {
			paymentModule.RegisterDepositRoutes(v1, authMiddleware)
		}

		if mtnCfg.Disbursement.IsConfigured() {
			paymentModule.RegisterWithdrawalRoutes(v1, authMiddleware)
		}

		// The callback path carries a shared secret (MTN_CALLBACK_SECRET). Without
		// one configured, the route is not mounted at all.
		if mtnCfg.CallbackSecret != "" {
			v1.POST("/payments/webhooks/mtn/:secret", mtn.RequireCallbackSecret(mtnCfg.CallbackSecret, mtn.WebhookHandler(
				mtn.SettlementProcessor(app.Database.SQL(), paymentModule.Repository(), client),
			)))
		}
	}
}
