package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	"github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
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
	email := "pay_test_" + userID.String()[:8] + "@example.com"

	_, err := db.Exec(`
INSERT INTO users (id, email, password_hash, email_verified, status, created_at, updated_at)
VALUES ($1, $2, 'dummyhash', true, 'ACTIVE', $3, $4)
`, userID, email, now, now)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}

	_, err = db.Exec(`
INSERT INTO wallets (id, user_id, available_balance, locked_balance, currency, status, created_at, updated_at)
VALUES ($1, $2, 0.0, 0.0, 'USD', 'ACTIVE', $3, $4)
`, walletID, userID, now, now)
	if err != nil {
		t.Fatalf("failed to insert test wallet: %v", err)
	}

	return userID, walletID
}

func TestPostgresRepository_CRUD(t *testing.T) {
	db := getTestDB(t)
	repo := postgres.New(db)
	ctx := context.Background()

	userID, walletID := createTestUserAndWallet(t, db)
	defer func() { _, _ = db.Exec("DELETE FROM users WHERE id = $1", userID) }()

	p, err := domain.NewPayment(
		userID,
		walletID,
		domain.DirectionDeposit,
		"BANK_TRANSFER",
		decimal.NewFromFloat(120.5),
		"USD",
		"REF-PAY-TEST-001",
		"integration test deposit",
		map[string]any{"source": "test_runner"},
	)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}

	t.Run("Create", func(t *testing.T) {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("FindByID", func(t *testing.T) {
		found, err := repo.FindByID(ctx, p.ID)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if found.UserID != userID || found.WalletID != walletID {
			t.Errorf("unexpected owner/wallet: %v / %v", found.UserID, found.WalletID)
		}
		if !found.Amount.Equal(p.Amount) {
			t.Errorf("expected amount %v, got %v", p.Amount, found.Amount)
		}
		if found.Direction != domain.DirectionDeposit || found.Status != domain.StatusPending {
			t.Errorf("unexpected direction/status: %s/%s", found.Direction, found.Status)
		}
		if found.Metadata["source"] != "test_runner" {
			t.Errorf("expected metadata source test_runner, got %v", found.Metadata["source"])
		}
	})

	t.Run("FindByReference", func(t *testing.T) {
		found, err := repo.FindByReference(ctx, "REF-PAY-TEST-001")
		if err != nil {
			t.Fatalf("FindByReference: %v", err)
		}
		if found.ID != p.ID {
			t.Errorf("expected ID %v, got %v", p.ID, found.ID)
		}
	})

	t.Run("FindByIDForUpdate within transaction", func(t *testing.T) {
		sqlTx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = sqlTx.Rollback() }()

		found, err := repo.WithTx(sqlTx).FindByIDForUpdate(ctx, p.ID)
		if err != nil {
			t.Fatalf("FindByIDForUpdate: %v", err)
		}
		if found.ID != p.ID {
			t.Errorf("expected ID %v, got %v", p.ID, found.ID)
		}
	})

	t.Run("Update status and provider reference", func(t *testing.T) {
		if err := p.TransitionTo(domain.StatusProcessing); err != nil {
			t.Fatalf("transition: %v", err)
		}
		p.SetProviderReference("PROV-123")

		if err := repo.Update(ctx, p); err != nil {
			t.Fatalf("Update: %v", err)
		}

		found, err := repo.FindByID(ctx, p.ID)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if found.Status != domain.StatusProcessing {
			t.Errorf("expected PROCESSING, got %s", found.Status)
		}
		if found.ProviderReference != "PROV-123" {
			t.Errorf("expected provider reference PROV-123, got %q", found.ProviderReference)
		}
	})

	t.Run("FindByUserID", func(t *testing.T) {
		list, err := repo.FindByUserID(ctx, userID, 10, 0)
		if err != nil {
			t.Fatalf("FindByUserID: %v", err)
		}
		if len(list) != 1 || list[0].ID != p.ID {
			t.Fatalf("expected one payment for user, got %d", len(list))
		}
	})

	t.Run("Update of missing payment returns not found", func(t *testing.T) {
		missing, err := domain.NewPayment(userID, walletID, domain.DirectionWithdrawal, "CARD", decimal.NewFromInt(1), "USD", "", "", nil)
		if err != nil {
			t.Fatalf("NewPayment: %v", err)
		}
		if err := repo.Update(ctx, missing); err != postgres.ErrPaymentNotFound {
			t.Fatalf("expected ErrPaymentNotFound, got %v", err)
		}
	})
}
