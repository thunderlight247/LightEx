package kyc

import (
	"context"
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/handler"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/service"
)

type Module struct {
	handler *handler.Handler
	service *service.Service
}

func New(db *sql.DB) *Module {
	svc := service.New(postgres.New(db))
	return &Module{handler: handler.New(svc), service: svc}
}

// PaymentGate exposes the KYC check that deposit and withdrawal routes require.
func (m *Module) PaymentGate() interface {
	PaymentAllowed(context.Context, uuid.UUID) (bool, error)
} {
	return m.service
}

func (m *Module) RegisterRoutes(router *gin.RouterGroup, auth gin.HandlerFunc) {
	m.handler.RegisterRoutes(router, auth)
}
