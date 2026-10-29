package postgres

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
	ledgerrepo "github.com/light-group/light-ex-backend/internal/modules/ledger/repository"
)

// DBTX is implemented by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Repository struct {
	db DBTX
}

// Compile-time interface check.
var _ ledgerrepo.Repository = (*Repository)(nil)

// New creates a PostgreSQL ledger repository.
func New(db DBTX) *Repository {
	return &Repository{db: db}
}

// Create inserts a ledger entry.
func (r *Repository) Create(ctx context.Context, e *domain.Entry) error {
	const query = `
INSERT INTO ledger_entries (
	id,
	transaction_id,
	wallet_id,
	entry_type,
	amount,
	currency,
	created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
`

	_, err := r.db.ExecContext(
		ctx,
		query,
		e.ID,
		e.TransactionID,
		e.WalletID,
		string(e.EntryType),
		e.Amount,
		e.Currency,
		e.CreatedAt,
	)
	return err
}

// FindByTransactionID returns the entries posted by a transaction, oldest first.
func (r *Repository) FindByTransactionID(ctx context.Context, transactionID uuid.UUID) ([]*domain.Entry, error) {
	const query = `
SELECT
	id,
	transaction_id,
	wallet_id,
	entry_type,
	amount,
	currency,
	created_at
FROM ledger_entries
WHERE transaction_id = $1
ORDER BY created_at ASC, id ASC
`

	rows, err := r.db.QueryContext(ctx, query, transactionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*domain.Entry
	for rows.Next() {
		var e domain.Entry
		var entryType string
		if err := rows.Scan(
			&e.ID,
			&e.TransactionID,
			&e.WalletID,
			&entryType,
			&e.Amount,
			&e.Currency,
			&e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.EntryType = domain.EntryType(entryType)
		entries = append(entries, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
