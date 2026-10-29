package payment

import (
	"database/sql"

	"github.com/light-group/light-ex-backend/internal/modules/payment/repository"
	"github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/payment/service"
)

type Module struct {
	repo    repository.Repository
	service *service.Service
}

// New creates the complete payment module.
func New(db *sql.DB) *Module {
	repo := postgres.New(db)
	svc := service.New(repo)

	return &Module{
		repo:    repo,
		service: svc,
	}
}

// Service returns the payment service for cross-module coordination.
func (m *Module) Service() *service.Service {
	return m.service
}

// Repository returns the underlying payment repository.
func (m *Module) Repository() repository.Repository {
	return m.repo
}
