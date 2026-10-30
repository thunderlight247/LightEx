package handler

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	provider "github.com/light-group/light-ex-backend/internal/modules/payment/provider"
	"github.com/light-group/light-ex-backend/internal/modules/payment/service"
	"github.com/light-group/light-ex-backend/internal/platform/config"
	"github.com/light-group/light-ex-backend/internal/platform/database"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"

	"github.com/joho/godotenv"
)

type fakeHTTPInitiator struct {
	collectResult  provider.Result
	collectErr     error
	disburseResult provider.Result
	disburseErr    error

	collectCalls  int
	disburseCalls int
	lastCollect   provider.Request
	lastDisburse  provider.Request
}

func (f *fakeHTTPInitiator) Collect(_ context.Context, req provider.Request) (provider.Result, error) {
	f.collectCalls++
	f.lastCollect = req
	return f.collectResult, f.collectErr
}

func (f *fakeHTTPInitiator) Disburse(_ context.Context, req provider.Request) (provider.Result, error) {
	f.disburseCalls++
	f.lastDisburse = req
	return f.disburseResult, f.disburseErr
}

func getHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()

	_ = godotenv.Load("../../../../.env")
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("skipping db integration test: failed to load config: %v", err)
	}

	dbWrapper, err := database.New(cfg.Database)
	if err != nil {
		t.Skipf("skipping db integration test: could not connect to PostgreSQL: %v", err)
	}

	return dbWrapper.SQL()
}

func seedHandlerUserWallet(t *testing.T, db *sql.DB, balance string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	now := time.Now().UTC()
	email := "payment_handler_" + userID.String()[:8] + "@example.com"

	if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, email, password_hash, email_verified, status, created_at, updated_at)
VALUES ($1, $2, 'dummyhash', true, 'ACTIVE', $3, $4)`, userID, email, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM ledger_entries WHERE wallet_id = $1`, walletID)
		_, _ = db.ExecContext(ctx, `DELETE FROM transactions WHERE wallet_id = $1`, walletID)
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE wallet_id = $1`, walletID)
		_, _ = db.ExecContext(ctx, `DELETE FROM wallets WHERE id = $1`, walletID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	if _, err := db.ExecContext(ctx, `
INSERT INTO wallets (id, user_id, available_balance, locked_balance, currency, status, created_at, updated_at)
VALUES ($1, $2, $3, 0, 'USD', 'ACTIVE', $4, $5)`, walletID, userID, balance, now, now); err != nil {
		t.Fatalf("insert wallet: %v", err)
	}

	return userID, walletID
}

func newTestRouter(actingUserID string, h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	authStub := func(c *gin.Context) {
		c.Set(middleware.ContextUserID, actingUserID)
		c.Next()
	}

	h.RegisterDepositRoute(router.Group(""), authStub)
	h.RegisterWithdrawalRoute(router.Group(""), authStub)

	return router
}

func postJSON(t *testing.T, router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	return res
}

func TestDepositHandler_CreatesProcessingPayment(t *testing.T) {
	db := getHandlerTestDB(t)
	userID, walletID := seedHandlerUserWallet(t, db, "100")

	fake := &fakeHTTPInitiator{collectResult: provider.Result{ProviderReference: "prov-ref-1", Status: provider.StatusPending}}
	router := newTestRouter(userID.String(), New(service.NewCollector(service.NewSQLDepositStore(db), fake)))

	res := postJSON(t, router, "/payments/deposits", `{"wallet_id":"`+walletID.String()+`","amount":"25.50","currency":"USD","phone":"+233241234567"}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", res.Code, res.Body.String())
	}

	if !strings.Contains(res.Body.String(), `"status":"PROCESSING"`) {
		t.Fatalf("expected PROCESSING status in body: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"provider_reference":"prov-ref-1"`) {
		t.Fatalf("expected provider reference in body: %s", res.Body.String())
	}

	if fake.collectCalls != 1 {
		t.Fatalf("expected one collect call, got %d", fake.collectCalls)
	}
	if fake.lastCollect.Phone != "+233241234567" || !fake.lastCollect.Amount.Equal(decimal.RequireFromString("25.50")) {
		t.Fatalf("provider request not built from request body: %+v", fake.lastCollect)
	}

	var paymentCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payments WHERE wallet_id = $1 AND status = 'PROCESSING'`, walletID).Scan(&paymentCount); err != nil {
		t.Fatalf("read payments: %v", err)
	}
	if paymentCount != 1 {
		t.Fatalf("expected one PROCESSING payment persisted, got %d", paymentCount)
	}
}

func TestDepositHandler_RejectsInvalidRequestsBeforeProvider(t *testing.T) {
	db := getHandlerTestDB(t)
	userID, walletID := seedHandlerUserWallet(t, db, "100")

	fake := &fakeHTTPInitiator{collectResult: provider.Result{ProviderReference: "prov-ref-1", Status: provider.StatusPending}}
	router := newTestRouter(userID.String(), New(service.NewCollector(service.NewSQLDepositStore(db), fake)))

	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid json", `{"wallet_id":`, http.StatusBadRequest},
		{"missing wallet", `{"amount":"5","currency":"USD","phone":"+233241234567"}`, http.StatusBadRequest},
		{"zero amount", `{"wallet_id":"` + walletID.String() + `","amount":"0","currency":"USD","phone":"+233"}`, http.StatusBadRequest},
		{"negative amount", `{"wallet_id":"` + walletID.String() + `","amount":"-5","currency":"USD","phone":"+233"}`, http.StatusBadRequest},
		{"short currency", `{"wallet_id":"` + walletID.String() + `","amount":"5","currency":"US","phone":"+233"}`, http.StatusBadRequest},
		{"missing phone", `{"wallet_id":"` + walletID.String() + `","amount":"5","currency":"USD"}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		res := postJSON(t, router, "/payments/deposits", tc.body)
		if res.Code != tc.want {
			t.Fatalf("%s: expected %d, got %d: %s", tc.name, tc.want, res.Code, res.Body.String())
		}
	}

	if fake.collectCalls != 0 {
		t.Fatalf("provider must not be called for rejected requests, got %d calls", fake.collectCalls)
	}
}

func TestDepositHandler_MapsWalletErrors(t *testing.T) {
	db := getHandlerTestDB(t)
	userID, walletID := seedHandlerUserWallet(t, db, "100")

	fake := &fakeHTTPInitiator{collectResult: provider.Result{ProviderReference: "prov-ref-1", Status: provider.StatusPending}}
	router := newTestRouter(userID.String(), New(service.NewCollector(service.NewSQLDepositStore(db), fake)))

	res := postJSON(t, router, "/payments/deposits", `{"wallet_id":"`+uuid.NewString()+`","amount":"5","currency":"USD","phone":"+233"}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown wallet, got %d: %s", res.Code, res.Body.String())
	}

	// The wallet of another authenticated user must be forbidden.
	otherUser := uuid.New()
	otherRouter := newTestRouter(otherUser.String(), New(service.NewCollector(service.NewSQLDepositStore(db), fake)))
	res = postJSON(t, otherRouter, "/payments/deposits", `{"wallet_id":"`+walletID.String()+`","amount":"5","currency":"USD","phone":"+233"}`)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for foreign wallet, got %d: %s", res.Code, res.Body.String())
	}

	if fake.collectCalls != 0 {
		t.Fatalf("provider must not be called on wallet errors, got %d calls", fake.collectCalls)
	}
}

func TestDepositHandler_ProviderFailureReturns500AndKeepsFailedPayment(t *testing.T) {
	db := getHandlerTestDB(t)
	userID, walletID := seedHandlerUserWallet(t, db, "100")

	fake := &fakeHTTPInitiator{collectErr: errors.New("sandbox unavailable")}
	router := newTestRouter(userID.String(), New(service.NewCollector(service.NewSQLDepositStore(db), fake)))

	res := postJSON(t, router, "/payments/deposits", `{"wallet_id":"`+walletID.String()+`","amount":"5","currency":"USD","phone":"+233"}`)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", res.Code, res.Body.String())
	}

	var paymentCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payments WHERE wallet_id = $1 AND status = 'FAILED'`, walletID).Scan(&paymentCount); err != nil {
		t.Fatalf("read payments: %v", err)
	}
	if paymentCount != 1 {
		t.Fatalf("expected one FAILED payment persisted, got %d", paymentCount)
	}
}

func TestWithdrawalHandler_ReservesFunds(t *testing.T) {
	db := getHandlerTestDB(t)
	userID, walletID := seedHandlerUserWallet(t, db, "100")

	fake := &fakeHTTPInitiator{disburseResult: provider.Result{ProviderReference: "wprov-ref-1", Status: provider.StatusPending}}
	router := newTestRouter(userID.String(), New(service.NewCollector(service.NewSQLDepositStore(db), fake)))

	res := postJSON(t, router, "/payments/withdrawals", `{"wallet_id":"`+walletID.String()+`","amount":"40","currency":"USD","phone":"+233241234567"}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"status":"PROCESSING"`) {
		t.Fatalf("expected PROCESSING status in body: %s", res.Body.String())
	}
	if fake.disburseCalls != 1 {
		t.Fatalf("expected one disburse call, got %d", fake.disburseCalls)
	}

	var available, locked decimal.Decimal
	if err := db.QueryRow(`SELECT available_balance, locked_balance FROM wallets WHERE id = $1`, walletID).Scan(&available, &locked); err != nil {
		t.Fatalf("read wallet: %v", err)
	}
	if !available.Equal(decimal.NewFromInt(60)) || locked.IsZero() {
		t.Fatalf("funds not reserved: available=%s locked=%s", available, locked)
	}
}
