package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/handler"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/service"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
)

type mockRepo struct {
	wallets       map[uuid.UUID]*domain.Wallet
	walletsByUser map[uuid.UUID]*domain.Wallet
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		wallets:       make(map[uuid.UUID]*domain.Wallet),
		walletsByUser: make(map[uuid.UUID]*domain.Wallet),
	}
}

func (m *mockRepo) Create(ctx context.Context, w *domain.Wallet) error {
	m.wallets[w.ID] = w
	m.walletsByUser[w.UserID] = w
	return nil
}

func (m *mockRepo) Update(ctx context.Context, w *domain.Wallet) error {
	m.wallets[w.ID] = w
	m.walletsByUser[w.UserID] = w
	return nil
}

func (m *mockRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	w, exists := m.wallets[id]
	if !exists {
		return nil, postgres.ErrWalletNotFound
	}
	clone := *w
	return &clone, nil
}

func (m *mockRepo) FindByUserID(ctx context.Context, userID uuid.UUID) (*domain.Wallet, error) {
	w, exists := m.walletsByUser[userID]
	if !exists {
		return nil, postgres.ErrWalletNotFound
	}
	clone := *w
	return &clone, nil
}

func (m *mockRepo) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	return m.FindByID(ctx, id)
}

func (m *mockRepo) FindByUserIDForUpdate(ctx context.Context, userID uuid.UUID) (*domain.Wallet, error) {
	return m.FindByUserID(ctx, userID)
}

func (m *mockRepo) ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	_, exists := m.walletsByUser[userID]
	return exists, nil
}

func setupTestRouter(repo *mockRepo, authenticatedUserID uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	svc := service.New(repo)
	h := handler.New(svc)

	// Mock auth middleware that injects the authenticatedUserID
	mockAuth := func(c *gin.Context) {
		if authenticatedUserID != uuid.Nil {
			c.Set(middleware.ContextUserID, authenticatedUserID.String())
		}
		c.Next()
	}

	v1 := router.Group("/api/v1")
	h.RegisterRoutes(v1, mockAuth)

	return router
}

func TestHandler_CreateWallet(t *testing.T) {
	repo := newMockRepo()
	userID := uuid.New()
	router := setupTestRouter(repo, userID)

	t.Run("creates wallet successfully with default USD", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewBuffer([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp handler.WalletResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.UserID != userID.String() {
			t.Errorf("expected userID %s, got %s", userID.String(), resp.UserID)
		}
		if resp.Currency != "USD" {
			t.Errorf("expected currency USD, got %s", resp.Currency)
		}
		if resp.AvailableBalance != "0.00000000" {
			t.Errorf("expected 0.00000000, got %s", resp.AvailableBalance)
		}
	})

	t.Run("conflict when wallet already exists", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewBuffer([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("expected status 409, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestHandler_GetMyWallet(t *testing.T) {
	repo := newMockRepo()
	userID := uuid.New()
	router := setupTestRouter(repo, userID)

	t.Run("not found before creation", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/me", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("found after creation", func(t *testing.T) {
		// Create wallet
		wModel, err := domain.NewWallet(userID, "GHS")
		if err != nil {
			t.Fatalf("failed to create model: %v", err)
		}
		_ = repo.Create(context.Background(), wModel)

		req, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/me", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp handler.WalletResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Currency != "GHS" {
			t.Errorf("expected currency GHS, got %s", resp.Currency)
		}
	})
}

func TestHandler_GetWalletByID(t *testing.T) {
	repo := newMockRepo()
	userA := uuid.New()
	userB := uuid.New()
	routerUserA := setupTestRouter(repo, userA)

	walletA, _ := domain.NewWallet(userA, "USD")
	walletB, _ := domain.NewWallet(userB, "USD")
	_ = repo.Create(context.Background(), walletA)
	_ = repo.Create(context.Background(), walletB)

	t.Run("user can view their own wallet", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/"+walletA.ID.String(), nil)
		w := httptest.NewRecorder()

		routerUserA.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("user cannot view another user's wallet (forbidden)", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/"+walletB.ID.String(), nil)
		w := httptest.NewRecorder()

		routerUserA.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("not found for non-existent wallet", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/wallets/"+uuid.New().String(), nil)
		w := httptest.NewRecorder()

		routerUserA.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
		}
	})
}
