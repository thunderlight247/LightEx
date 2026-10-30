package mtn

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/gin-gonic/gin"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

func TestParseCallback_Success(t *testing.T) {
	jsonPayload := `{
		"financialTransactionId": "ft-998877",
		"externalId": "DEP-100",
		"amount": "100.00",
		"currency": "GHS",
		"status": "SUCCESSFUL",
		"payer": {
			"partyIdType": "MSISDN",
			"partyId": "233240000000"
		}
	}`

	notif, err := ParseCallback([]byte(jsonPayload))
	if err != nil {
		t.Fatalf("unexpected error parsing callback: %v", err)
	}

	if notif.FinancialTransactionID != "ft-998877" {
		t.Errorf("expected financialTransactionId ft-998877, got %s", notif.FinancialTransactionID)
	}
	if notif.Status != "SUCCESSFUL" {
		t.Errorf("expected status SUCCESSFUL, got %s", notif.Status)
	}

	result := notif.Result()
	if result.Status != provider.StatusSucceeded {
		t.Errorf("expected StatusSucceeded, got %v", result.Status)
	}
	if result.ProviderReference != "ft-998877" {
		t.Errorf("expected ProviderReference ft-998877, got %s", result.ProviderReference)
	}
}

func TestParseCallback_Invalid(t *testing.T) {
	_, err := ParseCallback([]byte(""))
	if err == nil {
		t.Errorf("expected error on empty payload")
	}

	_, err = ParseCallback([]byte("not-json"))
	if err == nil {
		t.Errorf("expected error on non-json payload")
	}
}

func TestDeduplicatedWebhook_Postgres(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL to run PostgreSQL callback integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx := context.Background()
	schema := "callback_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migration, err := os.ReadFile("../../../../../migrations/000006_create_provider_callback_events.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE effects (reference TEXT, status TEXT)`); err != nil {
		t.Fatal(err)
	}
	var failRetry atomic.Bool
	failRetry.Store(true)
	process := func(ctx context.Context, tx *sql.Tx, n *CallbackNotification) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO effects VALUES ($1,$2)`, n.ExternalID, n.Status); err != nil {
			return err
		}
		if n.ExternalID == "retry" && failRetry.Load() {
			return errors.New("processing failed")
		}
		return nil
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/webhook", WebhookHandler(DeduplicatedProcessor(db, process)))
	send := func(payload string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
		r.ServeHTTP(w, req)
		return w.Code
	}
	pending := `{"externalId":"ref-1","status":"PENDING","amount":"10"}`
	success := `{"externalId":"ref-1","status":"SUCCESSFUL","amount":"10"}`
	if got := send(pending); got != http.StatusOK {
		t.Fatalf("pending: %d", got)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- send(success) }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent callback: %d", code)
		}
	}
	if got := send(`{"externalId":"ref-1","status":"SUCCESSFUL","amount":"11"}`); got != http.StatusInternalServerError {
		t.Fatalf("conflicting payload: %d", got)
	}
	for i := 0; i < 2; i++ {
		if got := send(`{"externalId":"retry","status":"FAILED"}`); got != http.StatusInternalServerError {
			t.Fatalf("failed callback: %d", got)
		}
	}
	failRetry.Store(false)
	if got := send(`{"externalId":"retry","status":"FAILED"}`); got != http.StatusOK {
		t.Fatalf("retry after rollback: %d", got)
	}
	if got := send(`{"status":"SUCCESSFUL"}`); got != http.StatusInternalServerError {
		t.Fatalf("missing reference: %d", got)
	}
	for _, table := range []string{"effects", "provider_callback_events"} {
		var count int
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Fatalf("%s: expected three committed rows, got %d", table, count)
		}
	}
}

func TestWebhookHandler_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var receivedNotif *CallbackNotification
	processor := func(c *gin.Context, notif *CallbackNotification) error {
		receivedNotif = notif
		return nil
	}

	r := gin.New()
	r.POST("/api/v1/payments/webhook/mtn", WebhookHandler(processor))

	payload := `{"financialTransactionId":"fin-1","status":"SUCCESSFUL","externalId":"REF-1"}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments/webhook/mtn", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
	if receivedNotif == nil || receivedNotif.FinancialTransactionID != "fin-1" {
		t.Fatalf("expected processor to receive notification, got %+v", receivedNotif)
	}
}
