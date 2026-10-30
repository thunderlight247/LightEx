package handler

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/domain"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider/simulated"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/service"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiquidityHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	user := uuid.New()
	auth := func(c *gin.Context) {
		if c.GetHeader("Authorization") != "test" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(middleware.ContextUserID, user.String())
		c.Next()
	}
	New(service.New(simulated.New(simulated.Success))).RegisterRoutes(r.Group("/api/v1"), auth)
	send := func(method, path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	base := "/api/v1/simulated-liquidity"
	if w := send("POST", base+"/quotes", `{}`, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := send("POST", base+"/quotes", `{"from":"GHS","to":"USDC","amount":"100"}`, "test")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var q domain.Quote
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil || !q.Simulated {
		t.Fatal(q, err)
	}
	body := `{"quote_id":"` + q.ID.String() + `","idempotency_key":"test-order"}`
	w = send("POST", base+"/orders", body, "test")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var order domain.Order
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	w = send("POST", base+"/orders", body, "test")
	var retry domain.Order
	_ = json.Unmarshal(w.Body.Bytes(), &retry)
	if w.Code != 200 || retry.ID != order.ID {
		t.Fatal(w.Code, w.Body.String())
	}
	w = send("GET", base+"/orders/"+order.ID.String(), "", "test")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	user = uuid.New()
	w = send("GET", base+"/orders/"+order.ID.String(), "", "test")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = send("POST", base+"/quotes", `{"from":"GHS","to":"USDC","amount":"-1"}`, "test")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
