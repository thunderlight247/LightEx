package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/domain"
	"github.com/light-group/light-ex-backend/internal/platform/bootstrap"
	"github.com/light-group/light-ex-backend/internal/platform/config"
	"github.com/light-group/light-ex-backend/internal/platform/database"
	jwtservice "github.com/light-group/light-ex-backend/internal/platform/security/jwt"
)

func TestSimulatedLiquidityRoutesIsolation(t *testing.T) {
	_ = godotenv.Load("../../.env")
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("database configuration unavailable: %v", err)
	}
	db, err := database.New(cfg.Database)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	defer db.Close()
	user, wallet := uuid.New(), uuid.New()
	_, err = db.SQL().Exec(`INSERT INTO users (id,email,password_hash,email_verified,status,created_at,updated_at) VALUES ($1,$2,'test',true,'ACTIVE',now(),now())`, user, user.String()+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.SQL().Exec(`DELETE FROM users WHERE id=$1`, user); err != nil {
			t.Error(err)
		}
	}()
	_, err = db.SQL().Exec(`INSERT INTO wallets (id,user_id,available_balance,locked_balance,currency,status,created_at,updated_at) VALUES ($1,$2,100,0,'GHS','ACTIVE',now(),now())`, wallet, user)
	if err != nil {
		t.Fatal(err)
	}
	cfg.JWT.Secret = "test-only-secret"
	cfg.JWT.Issuer = "test"
	cfg.JWT.AccessTokenTTL = time.Hour
	token, err := jwtservice.New(cfg.JWT).GenerateAccessToken(user.String())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	cfg.App.Env = "development"
	r := gin.New()
	RegisterRoutes(r, &bootstrap.Application{Config: cfg, Database: db})
	send := func(path, body, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	path := "/api/v1/simulated-liquidity"
	if w := send(path+"/quotes", `{}`, ""); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	w := send(path+"/quotes", `{"from":"GHS","to":"USDC","amount":"100"}`, token)
	if w.Code != 201 {
		t.Fatalf("quote %d %s", w.Code, w.Body.String())
	}
	var q domain.Quote
	if err = json.Unmarshal(w.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	w = send(path+"/orders", `{"quote_id":"`+q.ID.String()+`","idempotency_key":"integration"}`, token)
	if w.Code != 200 {
		t.Fatalf("order %d %s", w.Code, w.Body.String())
	}
	var balance string
	var ledgerCount, txCount, paymentCount int
	if err = db.SQL().QueryRow(`SELECT available_balance::text FROM wallets WHERE id=$1`, wallet).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err = db.SQL().QueryRow(`SELECT (SELECT count(*) FROM ledger_entries WHERE wallet_id=$1),(SELECT count(*) FROM transactions WHERE wallet_id=$1),(SELECT count(*) FROM payments WHERE wallet_id=$1)`, wallet).Scan(&ledgerCount, &txCount, &paymentCount); err != nil {
		t.Fatal(err)
	}
	if balance != "100.00000000" || ledgerCount != 0 || txCount != 0 || paymentCount != 0 {
		t.Fatalf("simulation changed financial state: balance=%s ledger=%d tx=%d payments=%d", balance, ledgerCount, txCount, paymentCount)
	}
	cfg.App.Env = "production"
	prod := gin.New()
	RegisterRoutes(prod, &bootstrap.Application{Config: cfg, Database: db})
	req := httptest.NewRequest("POST", path+"/quotes", nil)
	rec := httptest.NewRecorder()
	prod.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("simulation exposed outside development: %d", rec.Code)
	}
}
