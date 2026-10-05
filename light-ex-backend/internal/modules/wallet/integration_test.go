package wallet_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	auth "github.com/light-group/light-ex-backend/internal/modules/auth"
	wallet "github.com/light-group/light-ex-backend/internal/modules/wallet"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/handler"
	"github.com/light-group/light-ex-backend/internal/platform/config"
	"github.com/light-group/light-ex-backend/internal/platform/database"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
	jwtservice "github.com/light-group/light-ex-backend/internal/platform/security/jwt"
)

type loginResponse struct {
	AccessToken string `json:"access_token"`
}

func TestWallet_FullIntegration(t *testing.T) {
	_ = godotenv.Load("../../../.env")
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("skipping integration test: config load failed: %v", err)
	}

	dbWrapper, err := database.New(cfg.Database)
	if err != nil {
		t.Skipf("skipping integration test: could not connect to PostgreSQL: %v", err)
	}
	db := dbWrapper.SQL()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	jwtService := jwtservice.New(cfg.JWT)
	authMiddleware := middleware.Authentication(jwtService)

	authModule := auth.New(cfg, db)
	authModule.RegisterRoutes(v1)

	walletModule := wallet.New(db)
	walletModule.RegisterRoutes(v1, authMiddleware)

	// Step 1: Register a new user
	uniqueID := uuid.New().String()[:8]
	testEmail := fmt.Sprintf("wallet_integration_%s@example.com", uniqueID)
	testPassword := "VeryStrongPassword123!"

	registerPayload := fmt.Sprintf(`{"email":"%s","password":"%s"}`, testEmail, testPassword)
	regReq, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(registerPayload))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	router.ServeHTTP(regRec, regReq)

	if regRec.Code != http.StatusCreated {
		t.Fatalf("failed to register test user: %d: %s", regRec.Code, regRec.Body.String())
	}

	// Clean up user and associated records after test
	defer func() {
		_, _ = db.Exec("DELETE FROM users WHERE email = $1", testEmail)
	}()

	// Step 2: Login to obtain JWT
	loginPayload := fmt.Sprintf(`{"email":"%s","password":"%s"}`, testEmail, testPassword)
	loginReq, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(loginPayload))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("failed to login: %d: %s", loginRec.Code, loginRec.Body.String())
	}

	var loginResp loginResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("failed to unmarshal login response: %v", err)
	}

	bearerHeader := "Bearer " + loginResp.AccessToken

	// Step 3: GET /wallets/me before creating a wallet -> 404
	getMeReq, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/me", nil)
	getMeReq.Header.Set("Authorization", bearerHeader)
	getMeRec := httptest.NewRecorder()
	router.ServeHTTP(getMeRec, getMeReq)

	if getMeRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for uncreated wallet, got %d", getMeRec.Code)
	}

	// Step 4: POST /wallets -> creates wallet
	createReq, _ := http.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewBufferString(`{"currency":"USD"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", bearerHeader)
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", createRec.Code, createRec.Body.String())
	}

	var createdWallet handler.WalletResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &createdWallet); err != nil {
		t.Fatalf("failed to parse wallet response: %v", err)
	}

	if createdWallet.Currency != "USD" {
		t.Errorf("expected currency USD, got %s", createdWallet.Currency)
	}
	if createdWallet.AvailableBalance != "0.00000000" {
		t.Errorf("expected 0.00000000, got %s", createdWallet.AvailableBalance)
	}
	if createdWallet.Status != "ACTIVE" {
		t.Errorf("expected status ACTIVE, got %s", createdWallet.Status)
	}

	// Step 5: Duplicate POST /wallets -> 409 Conflict
	dupReq, _ := http.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewBufferString(`{"currency":"USD"}`))
	dupReq.Header.Set("Content-Type", "application/json")
	dupReq.Header.Set("Authorization", bearerHeader)
	dupRec := httptest.NewRecorder()
	router.ServeHTTP(dupRec, dupReq)

	if dupRec.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for duplicate wallet, got %d", dupRec.Code)
	}

	// Step 6: GET /wallets/me -> 200 OK
	getMeReq2, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/me", nil)
	getMeReq2.Header.Set("Authorization", bearerHeader)
	getMeRec2 := httptest.NewRecorder()
	router.ServeHTTP(getMeRec2, getMeReq2)

	if getMeRec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", getMeRec2.Code, getMeRec2.Body.String())
	}

	var fetchedWallet handler.WalletResponse
	if err := json.Unmarshal(getMeRec2.Body.Bytes(), &fetchedWallet); err != nil {
		t.Fatalf("failed to parse wallet response: %v", err)
	}
	if fetchedWallet.ID != createdWallet.ID {
		t.Errorf("expected wallet ID %s, got %s", createdWallet.ID, fetchedWallet.ID)
	}

	// Step 7: GET /wallets/:id -> 200 OK
	getByIdReq, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/"+createdWallet.ID, nil)
	getByIdReq.Header.Set("Authorization", bearerHeader)
	getByIdRec := httptest.NewRecorder()
	router.ServeHTTP(getByIdRec, getByIdReq)

	if getByIdRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", getByIdRec.Code, getByIdRec.Body.String())
	}

	// Step 8: Update wallet balance in DB and check GET /wallets/me reflects updated decimal values
	_, err = db.Exec(
		"UPDATE wallets SET available_balance = $1, locked_balance = $2, updated_at = $3 WHERE id = $4",
		"1250.50000000",
		"200.25000000",
		time.Now().UTC(),
		createdWallet.ID,
	)
	if err != nil {
		t.Fatalf("failed to update balances in DB: %v", err)
	}

	getMeReq3, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/me", nil)
	getMeReq3.Header.Set("Authorization", bearerHeader)
	getMeRec3 := httptest.NewRecorder()
	router.ServeHTTP(getMeRec3, getMeReq3)

	if getMeRec3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", getMeRec3.Code, getMeRec3.Body.String())
	}

	var balanceWallet handler.WalletResponse
	if err := json.Unmarshal(getMeRec3.Body.Bytes(), &balanceWallet); err != nil {
		t.Fatalf("failed to parse balance wallet response: %v", err)
	}

	if balanceWallet.AvailableBalance != "1250.50000000" {
		t.Errorf("expected 1250.50000000, got %s", balanceWallet.AvailableBalance)
	}
	if balanceWallet.LockedBalance != "200.25000000" {
		t.Errorf("expected 200.25000000, got %s", balanceWallet.LockedBalance)
	}
	if balanceWallet.TotalBalance != "1450.75000000" {
		t.Errorf("expected 1450.75000000, got %s", balanceWallet.TotalBalance)
	}
}
