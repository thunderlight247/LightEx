package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	"github.com/light-group/light-ex-backend/internal/platform/config"
	"github.com/light-group/light-ex-backend/internal/platform/database"
)

func getSQLTestDB(t *testing.T) *sql.DB {
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

// errStore wraps SQLStore and injects a failure after the inner function has
// performed its writes, so the rollback can be observed in the database.
type failAfterStore struct {
	inner *SQLStore
}

var errInjected = errors.New("injected failure")

func (f failAfterStore) WithinTx(ctx context.Context, fn func(r Repositories) error) error {
	return f.inner.WithinTx(ctx, func(r Repositories) error {
		if err := fn(r); err != nil {
			return err
		}
		return errInjected
	})
}

func TestSQLStore_CompleteTransactionRollsBackOnFailure(t *testing.T) {
	db := getSQLTestDB(t)
	ctx := context.Background()

	userID := uuid.New()
	walletID := uuid.New()
	now := time.Now().UTC()
	email := "txsvc_" + userID.String()[:8] + "@example.com"

	if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, email, password_hash, email_verified, status, created_at, updated_at)
VALUES ($1, $2, 'dummyhash', true, 'ACTIVE', $3, $4)`, userID, email, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM ledger_entries WHERE wallet_id = $1`, walletID)
		_, _ = db.ExecContext(ctx, `DELETE FROM transactions WHERE wallet_id = $1`, walletID)
		_, _ = db.ExecContext(ctx, `DELETE FROM wallets WHERE id = $1`, walletID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	if _, err := db.ExecContext(ctx, `
INSERT INTO wallets (id, user_id, available_balance, locked_balance, currency, status, created_at, updated_at)
VALUES ($1, $2, 100, 0, 'USD', 'ACTIVE', $3, $4)`, walletID, userID, now, now); err != nil {
		t.Fatalf("insert wallet: %v", err)
	}

	store := NewSQLStore(db)
	created, err := New(store).CreateTransaction(ctx, walletID, domain.TransactionTypeDeposit,
		decimal.NewFromInt(30), "USD", "", "rollback test", nil)
	if err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	// Complete through a store that fails after the wallet credit and transaction update.
	_, err = New(failAfterStore{inner: store}).CompleteTransaction(ctx, created.ID)
	if !errors.Is(err, errInjected) {
		t.Fatalf("expected injected failure, got %v", err)
	}

	var balance decimal.Decimal
	if err := db.QueryRowContext(ctx, `SELECT available_balance FROM wallets WHERE id = $1`, walletID).Scan(&balance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if !balance.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("balance not rolled back: got %s, want 100", balance)
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM transactions WHERE id = $1`, created.ID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != string(domain.TransactionStatusPending) {
		t.Fatalf("transaction status not rolled back: got %s", status)
	}

	var ledgerCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE transaction_id = $1`, created.ID).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if ledgerCount != 0 {
		t.Fatalf("ledger entry not rolled back: count %d", ledgerCount)
	}

	// The real path should succeed and commit both changes.
	done, err := New(store).CompleteTransaction(ctx, created.ID)
	if err != nil {
		t.Fatalf("CompleteTransaction: %v", err)
	}
	if done.Status != domain.TransactionStatusCompleted {
		t.Fatalf("expected COMPLETED, got %s", done.Status)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE transaction_id = $1 AND entry_type = 'CREDIT'`, created.ID).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("expected 1 committed CREDIT ledger entry, got %d", ledgerCount)
	}
	if err := db.QueryRowContext(ctx, `SELECT available_balance FROM wallets WHERE id = $1`, walletID).Scan(&balance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if !balance.Equal(decimal.NewFromInt(130)) {
		t.Fatalf("expected committed balance 130, got %s", balance)
	}
}
