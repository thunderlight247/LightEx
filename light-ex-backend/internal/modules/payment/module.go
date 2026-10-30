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

	collector := service.NewCollector(service.NewSQLDepositStore(m.db), initiator)
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

// RegisterDepositRoutes mounts POST /payments/deposits when the initiation
// handler is attached.
func (m *Module) RegisterDepositRoutes(v1 *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	if m.handler == nil {
		return
	}

	m.handler.RegisterDepositRoute(v1, authMiddleware)
}

// RegisterWithdrawalRoutes mounts POST /payments/withdrawals when the
// initiation handler is attached.
func (m *Module) RegisterWithdrawalRoutes(v1 *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	if m.handler == nil {
		return
	}

	m.handler.RegisterWithdrawalRoute(v1, authMiddleware)
}
