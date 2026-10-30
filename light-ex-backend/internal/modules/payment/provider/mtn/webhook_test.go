package mtn

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/shopspring/decimal"

	paymentpostgres "github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
	paymentservice "github.com/light-group/light-ex-backend/internal/modules/payment/service"

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

func TestVerifyCallback_Postgres(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL for PostgreSQL integration")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx := context.Background()
	schema := "verification_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
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
	for _, file := range []string{"000001_create_users_table.up.sql", "000002_create_wallets_table.up.sql", "000005_create_payments_table.up.sql"} {
		migration, err := os.ReadFile("../../../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	user, wallet, payment, reference := uuid.New(), uuid.New(), uuid.New(), uuid.NewString()
	if _, err = db.ExecContext(ctx, `INSERT INTO users (id,email,password_hash,status,created_at,updated_at) VALUES ($1,'test@example.com','test','ACTIVE',now(),now())`, user); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO wallets (id,user_id,available_balance,locked_balance,currency,status,created_at,updated_at) VALUES ($1,$2,100,0,'GHS','ACTIVE',now(),now())`, wallet, user); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO payments (id,user_id,wallet_id,direction,method,amount,currency,status,reference,provider_reference,created_at,updated_at) VALUES ($1,$2,$3,'DEPOSIT','MTN_MOMO',50,'GHS','PROCESSING','PAY-DB',$4,now(),now())`, payment, user, wallet, reference); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/collection/token/":
			_, _ = w.Write([]byte(`{"access_token":"test","expires_in":3600}`))
		case "/collection/v1_0/requesttopay/" + reference:
			_, _ = w.Write([]byte(`{"externalId":"PAY-DB","amount":"50.00","currency":"GHS","status":"SUCCESSFUL"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result, err := VerifyCallback(ctx, paymentpostgres.New(db), NewClient(dummyConfig(server.URL, server.Client())), &CallbackNotification{ExternalID: "PAY-DB", Amount: "50", Currency: "GHS", Status: "SUCCESSFUL"})
	if err != nil || result.PaymentID != payment || result.ProviderReference != reference || result.Status != provider.StatusSucceeded {
		t.Fatalf("verify: %+v %v", result, err)
	}
	var status string
	var balance string
	if err = db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id=$1`, payment).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT available_balance::text FROM wallets WHERE id=$1`, wallet).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if status != "PROCESSING" || balance != "100.00000000" {
		t.Fatalf("verification changed financial data: %s %s", status, balance)
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

// settlementFixture holds an isolated schema with users, wallets, payments,
// transactions and ledger migrations applied, plus one PROCESSING payment.
type settlementFixture struct {
	db       *sql.DB
	user     uuid.UUID
	payment  uuid.UUID
	wallet   uuid.UUID
	provider string // provider_reference stored on the payment
}

func newSettlementFixture(t *testing.T, ctx context.Context, dsn string, deposit bool) *settlementFixture {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Cleanup order is LIFO: drop schema first, close the fixture db, close admin last.
	t.Cleanup(func() { _ = admin.Close() })
	schema := "settlement_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
	})
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
	t.Cleanup(func() { _ = db.Close() })
	for _, file := range []string{"000001_create_users_table.up.sql", "000002_create_wallets_table.up.sql", "000003_create_transactions_table.up.sql", "000004_create_ledger_entries_table.up.sql", "000005_create_payments_table.up.sql", "000006_create_provider_callback_events.up.sql"} {
		migration, err := os.ReadFile("../../../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	user, wallet, payment, providerRef := uuid.New(), uuid.New(), uuid.New(), uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id,email,password_hash,status,created_at,updated_at) VALUES ($1,'settle@example.com','test','ACTIVE',now(),now())`, user); err != nil {
		t.Fatal(err)
	}
	locked := 0
	// Withdrawals reserve funds at initiation: seed the outbound transaction and
	// locked balance so the settlement can settle or release the reservation.
	if !deposit {
		locked = 50
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO wallets (id,user_id,available_balance,locked_balance,currency,status,created_at,updated_at) VALUES ($1,$2,100,$3,'GHS','ACTIVE',now(),now())`, wallet, user, locked); err != nil {
		t.Fatal(err)
	}
	direction := "DEPOSIT"
	if !deposit {
		direction = "WITHDRAWAL"
		if _, err := db.ExecContext(ctx, `INSERT INTO transactions (id,wallet_id,type,amount,currency,reference,status,description,created_at,updated_at) VALUES ($1,$2,'WITHDRAWAL',50,'GHS','PAY-SETTLE','PENDING','withdrawal',now(),now())`, uuid.New(), wallet); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO payments (id,user_id,wallet_id,direction,method,amount,currency,status,reference,provider_reference,created_at,updated_at) VALUES ($1,$2,$3,$4,'MTN_MOMO',50,'GHS','PROCESSING','PAY-SETTLE',$5,now(),now())`, payment, user, wallet, direction, providerRef); err != nil {
		t.Fatal(err)
	}
	return &settlementFixture{db: db, user: user, payment: payment, wallet: wallet, provider: providerRef}
}

// mtnStatusServer serves token + provider status for both products under the
// fixture's stored provider reference.
func mtnStatusServer(t *testing.T, fx *settlementFixture, direction, status string, override func(b []byte) []byte) *httptest.Server {
	t.Helper()
	body := []byte(`{"externalId":"PAY-SETTLE","amount":"50.00","currency":"GHS","status":"` + status + `","financialTransactionId":"ft-1"}`)
	if override != nil {
		body = override(body)
	}
	product, path := "collection", "requesttopay"
	if direction == "WITHDRAWAL" {
		product, path = "disbursement", "transfer"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/collection/token/", "/disbursement/token/":
			_, _ = w.Write([]byte(`{"access_token":"test","expires_in":3600}`))
		case "/" + product + "/v1_0/" + path + "/" + fx.provider:
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// settlementRoute builds the full webhook pipeline against the fixture's database.
func settlementRoute(t *testing.T, fx *settlementFixture, server *httptest.Server) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/hook", WebhookHandler(SettlementProcessor(fx.db, paymentpostgres.New(fx.db), NewClient(dummyConfig(server.URL, server.Client())))))
	return r
}

func sendSettlement(r *gin.Engine, payload string) int {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(payload))
	r.ServeHTTP(w, req)
	return w.Code
}

func TestSettlementProcessor_DepositSuccess_Postgres(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	fx := newSettlementFixture(t, ctx, dsn, true)
	server := mtnStatusServer(t, fx, "DEPOSIT", "SUCCESSFUL", nil)
	r := settlementRoute(t, fx, server)
	success := `{"externalId":"PAY-SETTLE","amount":"50","currency":"GHS","status":"SUCCESSFUL","financialTransactionId":"ft-1"}`

	if got := sendSettlement(r, success); got != http.StatusOK {
		t.Fatalf("first callback: %d", got)
	}
	// Duplicate deliveries must be idempotent end to end.
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- sendSettlement(r, success) }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("duplicate callback: %d", code)
		}
	}

	var status, balance string
	if err := fx.db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id=$1`, fx.payment).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.QueryRowContext(ctx, `SELECT available_balance::text FROM wallets WHERE id=$1`, fx.wallet).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if status != "COMPLETED" || balance != "150.00000000" {
		t.Fatalf("deposit settlement wrong: status=%s balance=%s", status, balance)
	}
	var trans, entries int
	if err := fx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transactions WHERE reference='PAY-SETTLE' AND type='DEPOSIT' AND status='COMPLETED'`).Scan(&trans); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if trans != 1 || entries != 1 {
		t.Fatalf("expected one completed transaction and one ledger entry, got %d and %d", trans, entries)
	}
}

func TestSettlementProcessor_DepositPending_NoMoves(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	fx := newSettlementFixture(t, ctx, dsn, true)
	server := mtnStatusServer(t, fx, "DEPOSIT", "PENDING", nil)
	r := settlementRoute(t, fx, server)

	if got := sendSettlement(r, `{"externalId":"PAY-SETTLE","amount":"50","currency":"GHS","status":"PENDING"}`); got != http.StatusOK {
		t.Fatalf("pending callback: %d", got)
	}
	var status, balance string
	if err := fx.db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id=$1`, fx.payment).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.QueryRowContext(ctx, `SELECT available_balance::text FROM wallets WHERE id=$1`, fx.wallet).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if status != "PROCESSING" || balance != "100.00000000" {
		t.Fatalf("pending callback moved state: status=%s balance=%s", status, balance)
	}
}

func TestSettlementProcessor_DepositFailed_RejectedNoCredit(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	fx := newSettlementFixture(t, ctx, dsn, true)
	server := mtnStatusServer(t, fx, "DEPOSIT", "FAILED", nil)
	r := settlementRoute(t, fx, server)

	if got := sendSettlement(r, `{"externalId":"PAY-SETTLE","amount":"50","currency":"GHS","status":"FAILED"}`); got != http.StatusOK {
		t.Fatalf("failed callback: %d", got)
	}
	var status, balance string
	if err := fx.db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id=$1`, fx.payment).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.QueryRowContext(ctx, `SELECT available_balance::text FROM wallets WHERE id=$1`, fx.wallet).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if status != "FAILED" || balance != "100.00000000" {
		t.Fatalf("failed deposit wrong: status=%s balance=%s", status, balance)
	}
	// The FAILED event is recorded, so a later forged SUCCESSFUL for the same
	// reference cannot ride the dedup unique key.
	if got := sendSettlement(r, `{"externalId":"PAY-SETTLE","amount":"50","currency":"GHS","status":"SUCCESSFUL"}`); got != http.StatusInternalServerError {
		t.Fatalf("expected forged success to be rejected, got %d", got)
	}
	var entries int
	if err := fx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("failed deposit produced %d ledger entries", entries)
	}
}

// TestInitiateAndSettle_Postgres runs the full asynchronous deposit path with
// the real MTN client against a mock MTN server: initiation records PROCESSING
// with the provider reference, and the webhook then settles the callback.
func TestInitiateAndSettle_Postgres(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	fx := newSettlementFixture(t, ctx, dsn, true)

	var mu sync.Mutex
	record := map[string]string{} // externalId, amount, served back on lookup
	var callbackRefID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/collection/v1_0/requesttopay":
			var payload struct {
				ExternalID string `json:"externalId"`
				Amount     string `json:"amount"`
				Currency   string `json:"currency"`
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("bad requesttopay payload: %v", err)
			}
			mu.Lock()
			record = map[string]string{"externalId": payload.ExternalID, "amount": payload.Amount, "currency": payload.Currency}
			callbackRefID = r.Header.Get("X-Reference-Id")
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/collection/v1_0/requesttopay/"):
			mu.Lock()
			defer mu.Unlock()
			if len(record) == 0 {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"externalId":%q,"amount":%q,"currency":%q,"status":"SUCCESSFUL","financialTransactionId":"ft-e2e"}`,
				record["externalId"], record["amount"], record["currency"])))
		case r.URL.Path == "/collection/token/":
			_, _ = w.Write([]byte(`{"access_token":"test","expires_in":3600}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(dummyConfig(server.URL, server.Client()))
	initiated, err := paymentservice.NewCollector(paymentservice.NewSQLDepositStore(fx.db), client).InitiateDeposit(ctx, paymentservice.CollectionInput{
		DepositInput: paymentservice.DepositInput{
			UserID:   fx.user,
			WalletID: fx.wallet,
			Method:   ProviderName,
			Amount:   decimal.NewFromInt(30),
			Currency: "GHS",
		},
		Phone: "233240000000",
	})
	if err != nil {
		t.Fatalf("InitiateDeposit: %v", err)
	}
	mu.Lock()
	callbackID := callbackRefID
	mu.Unlock()
	if callbackID == "" || callbackID != initiated.ProviderReference {
		t.Fatalf("provider reference not the X-Reference-Id MTN received: sent %q got %q", callbackID, initiated.ProviderReference)
	}

	// The default wallet starts at 100 with the fixture's unrelated payment.
	route := settlementRoute(t, fx, server)
	if got := sendSettlement(route, fmt.Sprintf(
		`{"externalId":%q,"amount":"30","currency":"GHS","status":"SUCCESSFUL","financialTransactionId":"ft-e2e"}`,
		initiated.Payment.Reference)); got != http.StatusOK {
		t.Fatalf("settlement callback: %d", got)
	}

	var status, balance string
	if err := fx.db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id=$1`, initiated.Payment.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.QueryRowContext(ctx, `SELECT available_balance::text FROM wallets WHERE id=$1`, fx.wallet).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if status != "COMPLETED" || balance != "130.00000000" {
		t.Fatalf("end-to-end settlement wrong: status=%s balance=%s", status, balance)
	}
}

// TestInitiateWithdrawalAndSettle_Postgres runs the full asynchronous
// withdrawal path with the real MTN client against a mock MTN server:
// initiation reserves funds and persists the provider reference, and the
// webhook then debits the locked funds on the provider's SUCCESSFUL status.
func TestInitiateWithdrawalAndSettle_Postgres(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	fx := newSettlementFixture(t, ctx, dsn, true)

	var mu sync.Mutex
	record := map[string]string{} // externalId, amount, currency served back on lookup
	var transferRefID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/disbursement/v1_0/transfer":
			var payload struct {
				ExternalID string `json:"externalId"`
				Amount     string `json:"amount"`
				Currency   string `json:"currency"`
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("bad transfer payload: %v", err)
			}
			mu.Lock()
			record = map[string]string{"externalId": payload.ExternalID, "amount": payload.Amount, "currency": payload.Currency}
			transferRefID = r.Header.Get("X-Reference-Id")
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/disbursement/v1_0/transfer/"):
			mu.Lock()
			defer mu.Unlock()
			if len(record) == 0 {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"externalId":%q,"amount":%q,"currency":%q,"status":"SUCCESSFUL","financialTransactionId":"ft-wth"}`,
				record["externalId"], record["amount"], record["currency"])))
		case r.URL.Path == "/disbursement/token/":
			_, _ = w.Write([]byte(`{"access_token":"test","expires_in":3600}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(dummyConfig(server.URL, server.Client()))
	initiated, err := paymentservice.NewCollector(paymentservice.NewSQLDepositStore(fx.db), client).InitiateWithdrawal(ctx, paymentservice.CollectionInput{
		DepositInput: paymentservice.DepositInput{
			UserID:   fx.user,
			WalletID: fx.wallet,
			Method:   ProviderName,
			Amount:   decimal.NewFromInt(30),
			Currency: "GHS",
		},
		Phone: "233240000000",
	})
	if err != nil {
		t.Fatalf("InitiateWithdrawal: %v", err)
	}
	mu.Lock()
	transferID := transferRefID
	mu.Unlock()
	if transferID == "" || transferID != initiated.ProviderReference {
		t.Fatalf("provider reference not the X-Reference-Id MTN received: sent %q got %q", transferID, initiated.ProviderReference)
	}
	var available, locked string
	if err := fx.db.QueryRowContext(ctx, `SELECT available_balance::text, locked_balance::text FROM wallets WHERE id=$1`, fx.wallet).Scan(&available, &locked); err != nil {
		t.Fatal(err)
	}
	if available != "70.00000000" || locked != "30.00000000" {
		t.Fatalf("reservation wrong after initiation: available=%s locked=%s", available, locked)
	}

	route := settlementRoute(t, fx, server)
	if got := sendSettlement(route, fmt.Sprintf(
		`{"externalId":%q,"amount":"30","currency":"GHS","status":"SUCCESSFUL","financialTransactionId":"ft-wth"}`,
		initiated.Payment.Reference)); got != http.StatusOK {
		t.Fatalf("settlement callback: %d", got)
	}

	var status string
	if err := fx.db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id=$1`, initiated.Payment.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.QueryRowContext(ctx, `SELECT available_balance::text, locked_balance::text FROM wallets WHERE id=$1`, fx.wallet).Scan(&available, &locked); err != nil {
		t.Fatal(err)
	}
	if status != "COMPLETED" || available != "70.00000000" || locked != "0.00000000" {
		t.Fatalf("end-to-end withdrawal settlement wrong: status=%s available=%s locked=%s", status, available, locked)
	}
	var entries int
	if err := fx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 1 {
		t.Fatalf("expected one ledger entry after settlement, got %d", entries)
	}
}

// A successful deposit settles even when its provider callback races against a
// transaction row created between verification and settlement.
func TestSettlementProcessor_WithdrawalFailed_ReleasesLock(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	fx := newSettlementFixture(t, ctx, dsn, false)
	server := mtnStatusServer(t, fx, "WITHDRAWAL", "FAILED", nil)
	r := settlementRoute(t, fx, server)

	if got := sendSettlement(r, `{"externalId":"PAY-SETTLE","amount":"50","currency":"GHS","status":"FAILED"}`); got != http.StatusOK {
		t.Fatalf("failed callback: %d", got)
	}
	var status, available, locked string
	if err := fx.db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id=$1`, fx.payment).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.QueryRowContext(ctx, `SELECT available_balance::text, locked_balance::text FROM wallets WHERE id=$1`, fx.wallet).Scan(&available, &locked); err != nil {
		t.Fatal(err)
	}
	if status != "FAILED" || available != "150.00000000" || locked != "0.00000000" {
		t.Fatalf("withdrawal unlock wrong: status=%s available=%s locked=%s", status, available, locked)
	}
}

func TestSettlementProcessor_Mismatch_Rejected(t *testing.T) {
	dsn := os.Getenv("LIGHTEX_CALLBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIGHTEX_CALLBACK_TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	fx := newSettlementFixture(t, ctx, dsn, true)
	// Provider says 50, but the callback body tries to inflate the amount.
	server := mtnStatusServer(t, fx, "DEPOSIT", "SUCCESSFUL", nil)
	r := settlementRoute(t, fx, server)

	if got := sendSettlement(r, `{"externalId":"PAY-SETTLE","amount":"500","currency":"GHS","status":"SUCCESSFUL"}`); got != http.StatusInternalServerError {
		t.Fatalf("expected mismatched callback to be rejected, got %d", got)
	}
	// A different reference inside the callback must also fail verification.
	if got := sendSettlement(r, `{"externalId":"UNKNOWN-REF","amount":"50","currency":"GHS","status":"SUCCESSFUL"}`); got != http.StatusInternalServerError {
		t.Fatalf("expected unknown reference to be rejected, got %d", got)
	}
	var status, balance string
	if err := fx.db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id=$1`, fx.payment).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.QueryRowContext(ctx, `SELECT available_balance::text FROM wallets WHERE id=$1`, fx.wallet).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if status != "PROCESSING" || balance != "100.00000000" {
		t.Fatalf("mismatched callback mutated state: status=%s balance=%s", status, balance)
	}
}
