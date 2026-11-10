package payment

import (
	"database/sql"

	"github.com/gin-gonic/gin"

	"github.com/light-group/light-ex-backend/internal/modules/payment/handler"
	"github.com/light-group/light-ex-backend/internal/modules/payment/repository"
	"github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/payment/service"
)

type Module struct {
	repo    repository.Repository
	service *service.Service
	db      *sql.DB
	gate    handler.PaymentGate
	limits  service.Limits

	handler *handler.Handler
}

// New creates the complete payment module.
func New(db *sql.DB) *Module {
	repo := postgres.New(db)
	svc := service.New(repo)

	return &Module{
		repo:    repo,
		service: svc,
		db:      db,
	}
}

// WithInitiator attaches the HTTP initiation handler backed by the given
// external provider starter. It keeps the first handler if called repeatedly.
func (m *Module) WithInitiator(initiator service.ProviderInitiator) *Module {
	if initiator == nil || m.handler != nil {
		return m
	}

	collector := service.NewCollector(service.NewSQLDepositStore(m.db), initiator).WithLimits(m.limits)
	m.handler = handler.New(collector)

	return m
}

// Service returns the payment service for cross-module coordination.
func (m *Module) Service() *service.Service {
	return m.service
}

// Repository returns the underlying payment repository.
func (m *Module) Repository() repository.Repository {
	return m.repo
}

// WithLimits sets per-transaction deposit and withdrawal maximums. It must be
// called before WithInitiator, which builds the collector that enforces them.
func (m *Module) WithLimits(limits service.Limits) *Module {
	m.limits = limits
	return m
}

// WithKYCGate requires a passing KYC status before deposits and withdrawals
// are accepted. Routes are mounted with this gate after authentication.
func (m *Module) WithKYCGate(gate handler.PaymentGate) *Module {
	m.gate = gate
	return m
}

// gated composes authentication with the KYC gate. Without a gate, funds
// movement is never mounted.
func (m *Module) gated(authMiddleware gin.HandlerFunc) (gin.HandlerFunc, bool) {
	if m.gate == nil {
		return nil, false
	}
	return authMiddleware, true
}

// RegisterDepositRoutes mounts POST /payments/deposits when the initiation
// handler is attached and a KYC gate is configured.
func (m *Module) RegisterDepositRoutes(v1 *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	if m.handler == nil {
		return
	}
	auth, ok := m.gated(authMiddleware)
	if !ok {
		return
	}
	m.handler.RegisterDepositRoute(v1, auth, handler.RequireKYC(m.gate))
}

// RegisterWithdrawalRoutes mounts POST /payments/withdrawals when the
// initiation handler is attached and a KYC gate is configured.
func (m *Module) RegisterWithdrawalRoutes(v1 *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	if m.handler == nil {
		return
	}
	auth, ok := m.gated(authMiddleware)
	if !ok {
		return
	}
	m.handler.RegisterWithdrawalRoute(v1, auth, handler.RequireKYC(m.gate))
}
