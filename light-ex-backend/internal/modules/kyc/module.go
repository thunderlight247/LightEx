package kyc

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/handler"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/service"
)

type Module struct{ handler *handler.Handler }

func New(db *sql.DB) *Module {
	return &Module{handler: handler.New(service.New(postgres.New(db)))}
}
func (m *Module) RegisterRoutes(router *gin.RouterGroup, auth gin.HandlerFunc) {
	m.handler.RegisterRoutes(router, auth)
}
