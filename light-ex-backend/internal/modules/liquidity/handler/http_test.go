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

func TestSimulatedOutcomeHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []simulated.Scenario{simulated.Pending, simulated.Timeout} {
		for _, outcome := range []domain.Status{domain.StatusSucceeded, domain.StatusFailed} {
			t.Run(string(scenario)+"/"+string(outcome), func(t *testing.T) {
				r := gin.New()
				owner := uuid.New()
				user := owner
				auth := func(c *gin.Context) {
					if c.GetHeader("Authorization") != "test" {
						c.AbortWithStatus(http.StatusUnauthorized)
						return
					}
					c.Set(middleware.ContextUserID, user.String())
					c.Next()
				}
				New(service.New(simulated.New(scenario))).RegisterRoutes(r.Group("/api/v1"), auth)
				send := func(path, body, token string) *httptest.ResponseRecorder {
					req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", token)
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)
					return w
				}
				base := "/api/v1/simulated-liquidity"
				w := send(base+"/quotes", `{"from":"GHS","to":"USDC","amount":"100"}`, "test")
				if w.Code != http.StatusCreated {
					t.Fatalf("quote: %d %s", w.Code, w.Body.String())
				}
				var quote domain.Quote
				if err := json.Unmarshal(w.Body.Bytes(), &quote); err != nil {
					t.Fatal(err)
				}
				w = send(base+"/orders", `{"quote_id":"`+quote.ID.String()+`","idempotency_key":"flow"}`, "test")
				if w.Code != http.StatusOK {
					t.Fatalf("order: %d %s", w.Code, w.Body.String())
				}
				var order domain.Order
				if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil {
					t.Fatal(err)
				}
				outcomePath := base + "/orders/" + order.ID.String() + "/simulated-outcome"
				body := `{"status":"` + string(outcome) + `"}`
				if w := send(outcomePath, body, ""); w.Code != http.StatusUnauthorized {
					t.Fatalf("unauthenticated: %d", w.Code)
				}
				user = uuid.New()
				if w := send(outcomePath, body, "test"); w.Code != http.StatusNotFound {
					t.Fatalf("foreign order: %d", w.Code)
				}
				user = owner
				if w := send(outcomePath, `{"status":"PENDING"}`, "test"); w.Code != http.StatusBadRequest {
					t.Fatalf("invalid status: %d", w.Code)
				}
				if w := send(outcomePath, body, "test"); w.Code != http.StatusNoContent {
					t.Fatalf("set outcome: %d %s", w.Code, w.Body.String())
				}
				// A contradictory provider outcome cannot overwrite the first one, even before reconciliation.
				other := domain.StatusFailed
				if outcome == other {
					other = domain.StatusSucceeded
				}
				if w := send(outcomePath, `{"status":"`+string(other)+`"}`, "test"); w.Code != http.StatusConflict {
					t.Fatalf("contradictory outcome: %d", w.Code)
				}
				w = send(base+"/orders/"+order.ID.String()+"/reconcile", `{}`, "test")
				if w.Code != http.StatusOK {
					t.Fatalf("reconcile: %d %s", w.Code, w.Body.String())
				}
				var final domain.Order
				if err := json.Unmarshal(w.Body.Bytes(), &final); err != nil {
					t.Fatal(err)
				}
				if final.ID != order.ID || final.ProviderReference != order.ProviderReference || final.Status != outcome || final.Settlement != "NOT_VERIFIED" || !final.Simulated {
					t.Fatalf("unexpected outcome: %+v", final)
				}
				if w := send(outcomePath, body, "test"); w.Code != http.StatusConflict {
					t.Fatalf("terminal order: %d", w.Code)
				}
			})
		}
	}
}

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
