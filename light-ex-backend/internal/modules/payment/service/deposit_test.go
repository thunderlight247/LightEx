package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"

	paymentdomain "github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	paymentpostgres "github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/platform/config"
	"github.com/light-group/light-ex-backend/internal/platform/database"
)

func getDepositTestDB(t *testing.T) *sql.DB {
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

// seedUserWallet inserts a user and an active USD wallet with the given starting balance.
func seedUserWallet(t *testing.T, db *sql.DB, balance string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	now := time.Now().UTC()
	email := "deposit_" + userID.String()[:8] + "@example.com"

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

func readBalance(t *testing.T, db *sql.DB, walletID uuid.UUID) decimal.Decimal {
	t.Helper()

	var balance decimal.Decimal
	if err := db.QueryRowContext(context.Background(),
		`SELECT available_balance FROM wallets WHERE id = $1`, walletID).Scan(&balance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return balance
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()

	var n int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestDeposit_CompletesAndCommitsAllWrites(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	depositor := NewDepositor(NewSQLDepositStore(db))

	res, err := depositor.Deposit(context.Background(), DepositInput{
		UserID:   userID,
		WalletID: walletID,
		Method:   "BANK_TRANSFER",
		Amount:   decimal.NewFromInt(25),
		Currency: "USD",
	})
	if err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if res.Payment.Status != paymentdomain.StatusCompleted {
		t.Fatalf("expected payment COMPLETED, got %s", res.Payment.Status)
	}
	if res.Transaction.Status != "COMPLETED" {
		t.Fatalf("expected transaction COMPLETED, got %s", res.Transaction.Status)
	}

	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(125)) {
		t.Fatalf("expected balance 125, got %s", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ledger_entries WHERE transaction_id = $1 AND entry_type = 'CREDIT'`, res.Transaction.ID); n != 1 {
		t.Fatalf("expected 1 CREDIT ledger entry, got %d", n)
	}
}

func TestDeposit_RollsBackEverythingOnFailure(t *testing.T) {
	db := getDepositTestDB(t)
	userID, walletID := seedUserWallet(t, db, "100")
	ctx := context.Background()

	// Occupy the reference so the payment insert inside the unit of work fails
	// on the UNIQUE constraint after the wallet has been locked.
	const taken = "DEP-ROLLBACK-TEST-REF"
	existing, err := paymentdomain.NewPayment(userID, walletID, paymentdomain.DirectionDeposit,
		"CARD", decimal.NewFromInt(1), "USD", taken, "", nil)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	if err := paymentpostgres.New(db).Create(ctx, existing); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	depositor := NewDepositor(NewSQLDepositStore(db))
	_, err = depositor.Deposit(ctx, DepositInput{
		UserID:    userID,
		WalletID:  walletID,
		Method:    "BANK_TRANSFER",
		Amount:    decimal.NewFromInt(25),
		Currency:  "USD",
		Reference: taken,
	})
	if err == nil {
		t.Fatal("expected duplicate reference error, got nil")
	}

	if got := readBalance(t, db, walletID); !got.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("balance changed on failed deposit: got %s, want 100", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM transactions WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("transaction left behind after rollback: %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ledger_entries WHERE wallet_id = $1`, walletID); n != 0 {
		t.Fatalf("ledger entry left behind after rollback: %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM payments WHERE wallet_id = $1`, walletID); n != 1 {
		t.Fatalf("expected only the pre-seeded payment, got %d", n)
	}
}
