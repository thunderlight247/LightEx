package wallet

import (
	"database/sql"

	"github.com/gin-gonic/gin"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/handler"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/repository"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/service"
)

type Module struct {
	repo    repository.Repository
	service *service.Service
	handler *handler.Handler
}

// New creates the complete wallet module.
func New(db *sql.DB) *Module {
	repo := postgres.New(db)
	svc := service.New(repo)
	httpHandler := handler.New(svc)

	return &Module{
		repo:    repo,
		service: svc,
		handler: httpHandler,
	}
}

// Service returns the underlying wallet service for cross-module coordination.
func (m *Module) Service() *service.Service {
	return m.service
}

// Repository returns the underlying wallet repository.
func (m *Module) Repository() repository.Repository {
	return m.repo
}

// RegisterRoutes registers wallet routes on the provided router group using the auth middleware.
func (m *Module) RegisterRoutes(router *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	m.handler.RegisterRoutes(router, authMiddleware)
}
