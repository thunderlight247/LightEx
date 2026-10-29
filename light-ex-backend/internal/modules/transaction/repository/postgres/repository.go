package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
	txrepo "github.com/light-group/light-ex-backend/internal/modules/transaction/repository"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
)

// DBTX is implemented by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Repository struct {
	db DBTX
}

// Compile-time interface check.
var _ txrepo.Repository = (*Repository)(nil)

// New creates a PostgreSQL transaction repository.
func New(db DBTX) *Repository {
	return &Repository{
		db: db,
	}
}

// WithTx returns a new Repository instance scoped to the given transaction.
func (r *Repository) WithTx(tx *sql.Tx) *Repository {
	return &Repository{
		db: tx,
	}
}

func (r *Repository) Create(ctx context.Context, tx *domain.Transaction) error {
	const query = `
INSERT INTO transactions (
	id,
	wallet_id,
	type,
	amount,
	currency,
	reference,
	status,
	description,
	metadata,
	created_at,
	updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`

	metadataJSON, err := json.Marshal(tx.Metadata)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(
		ctx,
		query,
		tx.ID,
		tx.WalletID,
		string(tx.Type),
		tx.Amount,
		tx.Currency,
		tx.Reference,
		string(tx.Status),
		tx.Description,
		metadataJSON,
		tx.CreatedAt,
		tx.UpdatedAt,
	)

	return err
}

func (r *Repository) Update(ctx context.Context, tx *domain.Transaction) error {
	const query = `
UPDATE transactions
SET
	status = $2,
	description = $3,
	metadata = $4,
	updated_at = $5
WHERE id = $1
`

	metadataJSON, err := json.Marshal(tx.Metadata)
	if err != nil {
		return err
	}

	result, err := r.db.ExecContext(
		ctx,
		query,
		tx.ID,
		string(tx.Status),
		tx.Description,
		metadataJSON,
		tx.UpdatedAt,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTransactionNotFound
	}

	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	const query = `
SELECT
	id,
	wallet_id,
	type,
	amount,
	currency,
	reference,
	status,
	description,
	metadata,
	created_at,
	updated_at
FROM transactions
WHERE id = $1
`

	return r.scanTransaction(r.db.QueryRowContext(ctx, query, id))
}

func (r *Repository) FindByReference(ctx context.Context, ref string) (*domain.Transaction, error) {
	const query = `
SELECT
	id,
	wallet_id,
	type,
	amount,
	currency,
	reference,
	status,
	description,
	metadata,
	created_at,
	updated_at
FROM transactions
WHERE reference = $1
`

	return r.scanTransaction(r.db.QueryRowContext(ctx, query, ref))
}

func (r *Repository) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	const query = `
SELECT
	id,
	wallet_id,
	type,
	amount,
	currency,
	reference,
	status,
	description,
	metadata,
	created_at,
	updated_at
FROM transactions
WHERE id = $1
FOR UPDATE
`

	return r.scanTransaction(r.db.QueryRowContext(ctx, query, id))
}

func (r *Repository) FindByWalletID(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*domain.Transaction, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	const query = `
SELECT
	id,
	wallet_id,
	type,
	amount,
	currency,
	reference,
	status,
	description,
	metadata,
	created_at,
	updated_at
FROM transactions
WHERE wallet_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3
`

	rows, err := r.db.QueryContext(ctx, query, walletID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transactions []*domain.Transaction
	for rows.Next() {
		var (
			t             domain.Transaction
			txType        string
			status        string
			metadataBytes []byte
		)

		err := rows.Scan(
			&t.ID,
			&t.WalletID,
			&txType,
			&t.Amount,
			&t.Currency,
			&t.Reference,
			&status,
			&t.Description,
			&metadataBytes,
			&t.CreatedAt,
			&t.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		t.Type = domain.TransactionType(txType)
		t.Status = domain.TransactionStatus(status)

		if len(metadataBytes) > 0 {
			_ = json.Unmarshal(metadataBytes, &t.Metadata)
		}
		if t.Metadata == nil {
			t.Metadata = make(map[string]any)
		}

		transactions = append(transactions, &t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return transactions, nil
}

func (r *Repository) CountByWalletID(ctx context.Context, walletID uuid.UUID) (int64, error) {
	const query = `
SELECT COUNT(*)
FROM transactions
WHERE wallet_id = $1
`

	var count int64
	err := r.db.QueryRowContext(ctx, query, walletID).Scan(&count)
	return count, err
}

func (r *Repository) scanTransaction(row *sql.Row) (*domain.Transaction, error) {
	var (
		t             domain.Transaction
		txType        string
		status        string
		metadataBytes []byte
	)

	err := row.Scan(
		&t.ID,
		&t.WalletID,
		&txType,
		&t.Amount,
		&t.Currency,
		&t.Reference,
		&status,
		&t.Description,
		&metadataBytes,
		&t.CreatedAt,
		&t.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}

	t.Type = domain.TransactionType(txType)
	t.Status = domain.TransactionStatus(status)

	if len(metadataBytes) > 0 {
		_ = json.Unmarshal(metadataBytes, &t.Metadata)
	}
	if t.Metadata == nil {
		t.Metadata = make(map[string]any)
	}

	return &t, nil
}
