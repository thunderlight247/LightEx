package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
	walletrepo "github.com/light-group/light-ex-backend/internal/modules/wallet/repository"
)

var (
	ErrWalletNotFound = errors.New("wallet not found")
)

// DBTX is implemented by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(
		ctx context.Context,
		query string,
		args ...any,
	) (sql.Result, error)

	QueryRowContext(
		ctx context.Context,
		query string,
		args ...any,
	) *sql.Row
}

type Repository struct {
	db DBTX
}

// Compile-time interface check.
var _ walletrepo.Repository = (*Repository)(nil)

// New creates a PostgreSQL wallet repository.
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

func (r *Repository) Create(
	ctx context.Context,
	wallet *domain.Wallet,
) error {
	const query = `
INSERT INTO wallets (
	id,
	user_id,
	available_balance,
	locked_balance,
	currency,
	status,
	created_at,
	updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`

	_, err := r.db.ExecContext(
		ctx,
		query,
		wallet.ID,
		wallet.UserID,
		wallet.AvailableBalance,
		wallet.LockedBalance,
		wallet.Currency,
		string(wallet.Status),
		wallet.CreatedAt,
		wallet.UpdatedAt,
	)

	return err
}

func (r *Repository) Update(
	ctx context.Context,
	wallet *domain.Wallet,
) error {
	const query = `
UPDATE wallets
SET
	available_balance = $2,
	locked_balance = $3,
	currency = $4,
	status = $5,
	updated_at = $6
WHERE id = $1
`

	result, err := r.db.ExecContext(
		ctx,
		query,
		wallet.ID,
		wallet.AvailableBalance,
		wallet.LockedBalance,
		wallet.Currency,
		string(wallet.Status),
		wallet.UpdatedAt,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWalletNotFound
	}

	return nil
}

func (r *Repository) FindByID(
	ctx context.Context,
	id uuid.UUID,
) (*domain.Wallet, error) {
	const query = `
SELECT
	id,
	user_id,
	available_balance,
	locked_balance,
	currency,
	status,
	created_at,
	updated_at
FROM wallets
WHERE id = $1
`

	return r.scanWallet(r.db.QueryRowContext(ctx, query, id))
}

func (r *Repository) FindByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (*domain.Wallet, error) {
	const query = `
SELECT
	id,
	user_id,
	available_balance,
	locked_balance,
	currency,
	status,
	created_at,
	updated_at
FROM wallets
WHERE user_id = $1
`

	return r.scanWallet(r.db.QueryRowContext(ctx, query, userID))
}

func (r *Repository) FindByIDForUpdate(
	ctx context.Context,
	id uuid.UUID,
) (*domain.Wallet, error) {
	const query = `
SELECT
	id,
	user_id,
	available_balance,
	locked_balance,
	currency,
	status,
	created_at,
	updated_at
FROM wallets
WHERE id = $1
FOR UPDATE
`

	return r.scanWallet(r.db.QueryRowContext(ctx, query, id))
}

func (r *Repository) FindByUserIDForUpdate(
	ctx context.Context,
	userID uuid.UUID,
) (*domain.Wallet, error) {
	const query = `
SELECT
	id,
	user_id,
	available_balance,
	locked_balance,
	currency,
	status,
	created_at,
	updated_at
FROM wallets
WHERE user_id = $1
FOR UPDATE
`

	return r.scanWallet(r.db.QueryRowContext(ctx, query, userID))
}

func (r *Repository) ExistsByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (bool, error) {
	const query = `
SELECT EXISTS(
	SELECT 1
	FROM wallets
	WHERE user_id = $1
)
`

	var exists bool
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&exists)
	return exists, err
}

func (r *Repository) scanWallet(
	row *sql.Row,
) (*domain.Wallet, error) {
	var (
		w      domain.Wallet
		status string
	)

	err := row.Scan(
		&w.ID,
		&w.UserID,
		&w.AvailableBalance,
		&w.LockedBalance,
		&w.Currency,
		&status,
		&w.CreatedAt,
		&w.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}

	w.Status = domain.WalletStatus(status)
	return &w, nil
}
