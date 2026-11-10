package liquidity

import (
	"github.com/gin-gonic/gin"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/handler"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider/simulated"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/service"
)

type Module struct{ handler *handler.Handler }

func New(scenario simulated.Scenario) *Module {
	if scenario == "" {
		scenario = simulated.Success
	}
	return &Module{handler: handler.New(service.New(simulated.New(scenario)))}
}
func (m *Module) RegisterRoutes(router *gin.RouterGroup, auth gin.HandlerFunc) {
	m.handler.RegisterRoutes(router, auth)
}
