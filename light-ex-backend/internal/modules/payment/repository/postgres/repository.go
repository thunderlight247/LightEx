package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	paymentrepo "github.com/light-group/light-ex-backend/internal/modules/payment/repository"
)

var (
	ErrPaymentNotFound = errors.New("payment not found")
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
var _ paymentrepo.Repository = (*Repository)(nil)

// New creates a PostgreSQL payment repository.
func New(db DBTX) *Repository {
	return &Repository{db: db}
}

// WithTx returns a new Repository instance scoped to the given transaction.
func (r *Repository) WithTx(tx *sql.Tx) *Repository {
	return &Repository{db: tx}
}

const selectColumns = `
SELECT
	id,
	user_id,
	wallet_id,
	direction,
	method,
	amount,
	currency,
	status,
	reference,
	provider_reference,
	description,
	metadata,
	created_at,
	updated_at
FROM payments
`

func (r *Repository) Create(ctx context.Context, p *domain.Payment) error {
	const query = `
INSERT INTO payments (
	id,
	user_id,
	wallet_id,
	direction,
	method,
	amount,
	currency,
	status,
	reference,
	provider_reference,
	description,
	metadata,
	created_at,
	updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
`

	metadataJSON, err := json.Marshal(p.Metadata)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(
		ctx,
		query,
		p.ID,
		p.UserID,
		p.WalletID,
		string(p.Direction),
		p.Method,
		p.Amount,
		p.Currency,
		string(p.Status),
		p.Reference,
		nullableString(p.ProviderReference),
		p.Description,
		metadataJSON,
		p.CreatedAt,
		p.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, p *domain.Payment) error {
	const query = `
UPDATE payments
SET
	status = $2,
	provider_reference = $3,
	description = $4,
	metadata = $5,
	updated_at = $6
WHERE id = $1
`

	metadataJSON, err := json.Marshal(p.Metadata)
	if err != nil {
		return err
	}

	result, err := r.db.ExecContext(
		ctx,
		query,
		p.ID,
		string(p.Status),
		nullableString(p.ProviderReference),
		p.Description,
		metadataJSON,
		p.UpdatedAt,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPaymentNotFound
	}
	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	return r.scanPayment(r.db.QueryRowContext(ctx, selectColumns+"WHERE id = $1\n", id))
}

func (r *Repository) FindByReference(ctx context.Context, ref string) (*domain.Payment, error) {
	return r.scanPayment(r.db.QueryRowContext(ctx, selectColumns+"WHERE reference = $1\n", ref))
}

func (r *Repository) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	return r.scanPayment(r.db.QueryRowContext(ctx, selectColumns+"WHERE id = $1\nFOR UPDATE\n", id))
}

func (r *Repository) FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Payment, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.db.QueryContext(
		ctx,
		selectColumns+"WHERE user_id = $1\nORDER BY created_at DESC\nLIMIT $2 OFFSET $3\n",
		userID,
		limit,
		offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []*domain.Payment
	for rows.Next() {
		p, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return payments, nil
}

func (r *Repository) FindByStatusAndUpdatedBefore(ctx context.Context, status domain.Status, before time.Time, limit int) ([]*domain.Payment, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.db.QueryContext(
		ctx,
		selectColumns+"WHERE status = $1 AND updated_at <= $2\nORDER BY updated_at ASC\nLIMIT $3\n",
		string(status),
		before,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []*domain.Payment
	for rows.Next() {
		p, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return payments, nil
}

func (r *Repository) scanPayment(row *sql.Row) (*domain.Payment, error) {
	p, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPaymentNotFound
	}
	return p, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRow(s scanner) (*domain.Payment, error) {
	var (
		p                 domain.Payment
		direction         string
		status            string
		providerReference sql.NullString
		description       sql.NullString
		metadataBytes     []byte
	)

	err := s.Scan(
		&p.ID,
		&p.UserID,
		&p.WalletID,
		&direction,
		&p.Method,
		&p.Amount,
		&p.Currency,
		&status,
		&p.Reference,
		&providerReference,
		&description,
		&metadataBytes,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	p.Direction = domain.Direction(direction)
	p.Status = domain.Status(status)
	p.ProviderReference = providerReference.String
	p.Description = description.String

	if len(metadataBytes) > 0 {
		_ = json.Unmarshal(metadataBytes, &p.Metadata)
	}
	if p.Metadata == nil {
		p.Metadata = make(map[string]any)
	}

	return &p, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
