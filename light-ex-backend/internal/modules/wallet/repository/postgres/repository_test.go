package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
	"github.com/light-group/light-ex-backend/internal/modules/wallet/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/platform/config"
	"github.com/light-group/light-ex-backend/internal/platform/database"
)

func getTestDB(t *testing.T) *sql.DB {
	t.Helper()

	_ = godotenv.Load("../../../../../.env")
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

func createTestUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	email := "wallet_test_" + userID.String()[:8] + "@example.com"
	now := time.Now().UTC()

	const query = `
INSERT INTO users (id, email, password_hash, email_verified, status, created_at, updated_at)
VALUES ($1, $2, 'dummyhash', true, 'ACTIVE', $3, $4)
`
	_, err := db.Exec(query, userID, email, now, now)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}

	return userID
}

func cleanupTestUser(t *testing.T, db *sql.DB, userID uuid.UUID) {
	t.Helper()
	_, _ = db.Exec("DELETE FROM users WHERE id = $1", userID)
}

func TestPostgresRepository_CRUD(t *testing.T) {
	db := getTestDB(t)
	repo := postgres.New(db)
	ctx := context.Background()

	userID := createTestUser(t, db)
	defer cleanupTestUser(t, db, userID)

	w, err := domain.NewWallet(userID, "USD")
	if err != nil {
		t.Fatalf("failed to create domain wallet: %v", err)
	}

	t.Run("Create wallet", func(t *testing.T) {
		if err := repo.Create(ctx, w); err != nil {
			t.Fatalf("expected no error creating wallet, got: %v", err)
		}
	})

	t.Run("ExistsByUserID", func(t *testing.T) {
		exists, err := repo.ExistsByUserID(ctx, userID)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !exists {
			t.Errorf("expected wallet to exist for user")
		}

		notExists, err := repo.ExistsByUserID(ctx, uuid.New())
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if notExists {
			t.Errorf("expected false for non-existent user")
		}
	})

	t.Run("FindByID", func(t *testing.T) {
		found, err := repo.FindByID(ctx, w.ID)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if found.ID != w.ID {
			t.Errorf("expected wallet ID %v, got %v", w.ID, found.ID)
		}
		if found.UserID != userID {
			t.Errorf("expected user ID %v, got %v", userID, found.UserID)
		}
		if found.Currency != "USD" {
			t.Errorf("expected currency USD, got %v", found.Currency)
		}
		if !found.AvailableBalance.Equal(decimal.Zero) {
			t.Errorf("expected 0 available balance, got %v", found.AvailableBalance)
		}
		if !found.LockedBalance.Equal(decimal.Zero) {
			t.Errorf("expected 0 locked balance, got %v", found.LockedBalance)
		}
	})

	t.Run("FindByUserID", func(t *testing.T) {
		found, err := repo.FindByUserID(ctx, userID)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if found.ID != w.ID {
			t.Errorf("expected wallet ID %v, got %v", w.ID, found.ID)
		}
	})

	t.Run("Update balances", func(t *testing.T) {
		creditAmt := decimal.NewFromFloat(250.75)
		if err := w.Credit(creditAmt); err != nil {
			t.Fatalf("domain credit failed: %v", err)
		}

		lockAmt := decimal.NewFromFloat(50.25)
		if err := w.LockFunds(lockAmt); err != nil {
			t.Fatalf("domain lock failed: %v", err)
		}

		if err := repo.Update(ctx, w); err != nil {
			t.Fatalf("repo update failed: %v", err)
		}

		found, err := repo.FindByID(ctx, w.ID)
		if err != nil {
			t.Fatalf("failed to fetch updated wallet: %v", err)
		}

		expectedAvailable := decimal.NewFromFloat(200.50)
		expectedLocked := decimal.NewFromFloat(50.25)

		if !found.AvailableBalance.Equal(expectedAvailable) {
			t.Errorf("expected available %v, got %v", expectedAvailable, found.AvailableBalance)
		}
		if !found.LockedBalance.Equal(expectedLocked) {
			t.Errorf("expected locked %v, got %v", expectedLocked, found.LockedBalance)
		}
	})

	t.Run("FindByIDForUpdate in transaction", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("failed to begin tx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		txRepo := repo.WithTx(tx)
		lockedWallet, err := txRepo.FindByIDForUpdate(ctx, w.ID)
		if err != nil {
			t.Fatalf("FindByIDForUpdate failed: %v", err)
		}
		if lockedWallet.ID != w.ID {
			t.Errorf("expected wallet ID %v, got %v", w.ID, lockedWallet.ID)
		}

		_ = tx.Commit()
	})
}
