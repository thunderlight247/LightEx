package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	"github.com/light-group/light-ex-backend/internal/modules/transaction/repository/postgres"
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

func createTestUserAndWallet(t *testing.T, db *sql.DB) (uuid.UUID, uuid.UUID) {
	t.Helper()

	userID := uuid.New()
	walletID := uuid.New()
	now := time.Now().UTC()
	email := "tx_test_" + userID.String()[:8] + "@example.com"

	_, err := db.Exec(`
INSERT INTO users (id, email, password_hash, email_verified, status, created_at, updated_at)
VALUES ($1, $2, 'dummyhash', true, 'ACTIVE', $3, $4)
`, userID, email, now, now)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}

	_, err = db.Exec(`
INSERT INTO wallets (id, user_id, available_balance, locked_balance, currency, status, created_at, updated_at)
VALUES ($1, $2, 1000.0, 0.0, 'USD', 'ACTIVE', $3, $4)
`, walletID, userID, now, now)
	if err != nil {
		t.Fatalf("failed to insert test wallet: %v", err)
	}

	return userID, walletID
}

func cleanupTestUser(t *testing.T, db *sql.DB, userID uuid.UUID) {
	t.Helper()
	_, _ = db.Exec("DELETE FROM users WHERE id = $1", userID)
}

func TestPostgresRepository_CRUD(t *testing.T) {
	db := getTestDB(t)
	repo := postgres.New(db)
	ctx := context.Background()

	userID, walletID := createTestUserAndWallet(t, db)
	defer cleanupTestUser(t, db, userID)

	amount := decimal.NewFromFloat(150.75)
	metadata := map[string]any{"source": "test_runner", "batch": float64(1)}

	tx, err := domain.NewTransaction(
		walletID,
		domain.TransactionTypeDeposit,
		amount,
		"USD",
		"REF-TX-TEST-001",
		"Initial test deposit",
		metadata,
	)
	if err != nil {
		t.Fatalf("failed to create domain transaction: %v", err)
	}

	t.Run("Create transaction", func(t *testing.T) {
		if err := repo.Create(ctx, tx); err != nil {
			t.Fatalf("expected no error creating transaction, got: %v", err)
		}
	})

	t.Run("FindByID", func(t *testing.T) {
		found, err := repo.FindByID(ctx, tx.ID)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if found.ID != tx.ID {
			t.Errorf("expected ID %v, got %v", tx.ID, found.ID)
		}
		if found.WalletID != walletID {
			t.Errorf("expected wallet ID %v, got %v", walletID, found.WalletID)
		}
		if !found.Amount.Equal(amount) {
			t.Errorf("expected amount %v, got %v", amount, found.Amount)
		}
		if found.Reference != "REF-TX-TEST-001" {
			t.Errorf("expected reference REF-TX-TEST-001, got %s", found.Reference)
		}
		if found.Metadata["source"] != "test_runner" {
			t.Errorf("expected metadata source test_runner, got %v", found.Metadata["source"])
		}
	})

	t.Run("FindByReference", func(t *testing.T) {
		found, err := repo.FindByReference(ctx, "REF-TX-TEST-001")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if found.ID != tx.ID {
			t.Errorf("expected ID %v, got %v", tx.ID, found.ID)
		}
	})

	t.Run("FindByIDForUpdate within transaction", func(t *testing.T) {
		sqlTx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("failed to begin sql tx: %v", err)
		}
		defer func() { _ = sqlTx.Rollback() }()

		txRepo := repo.WithTx(sqlTx)
		found, err := txRepo.FindByIDForUpdate(ctx, tx.ID)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if found.ID != tx.ID {
			t.Errorf("expected ID %v, got %v", tx.ID, found.ID)
		}
	})

	t.Run("Update transaction status and metadata", func(t *testing.T) {
		if err := tx.MarkProcessing(); err != nil {
			t.Fatalf("failed to mark processing: %v", err)
		}
		tx.Metadata["note"] = "processing now"

		if err := repo.Update(ctx, tx); err != nil {
			t.Fatalf("failed to update transaction: %v", err)
		}

		found, err := repo.FindByID(ctx, tx.ID)
		if err != nil {
			t.Fatalf("failed to reload transaction: %v", err)
		}
		if found.Status != domain.TransactionStatusProcessing {
			t.Errorf("expected status PROCESSING, got %s", found.Status)
		}
		if found.Metadata["note"] != "processing now" {
			t.Errorf("expected metadata note to be updated")
		}
	})

	t.Run("FindByWalletID and CountByWalletID", func(t *testing.T) {
		// Create a second transaction
		tx2, err := domain.NewTransaction(
			walletID,
			domain.TransactionTypeWithdrawal,
			decimal.NewFromFloat(50.0),
			"USD",
			"REF-TX-TEST-002",
			"Withdrawal test",
			nil,
		)
		if err != nil {
			t.Fatalf("failed to create second tx: %v", err)
		}
		if err := repo.Create(ctx, tx2); err != nil {
			t.Fatalf("failed to persist second tx: %v", err)
		}

		count, err := repo.CountByWalletID(ctx, walletID)
		if err != nil {
			t.Fatalf("failed to count: %v", err)
		}
		if count != 2 {
			t.Errorf("expected count 2, got %d", count)
		}

		list, err := repo.FindByWalletID(ctx, walletID, 10, 0)
		if err != nil {
			t.Fatalf("failed to list: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("expected 2 transactions, got %d", len(list))
		}
		// Most recent first
		if list[0].Reference != "REF-TX-TEST-002" {
			t.Errorf("expected most recent tx first, got %s", list[0].Reference)
		}
	})
}
