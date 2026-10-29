// Package repository defines persistence contracts for the transaction
// module. It contains interfaces only and is independent of any database
// implementation.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/transaction/domain"
)

// Repository defines persistence operations for transactions.
type Repository interface {
	// Create persists a new transaction record.
	Create(ctx context.Context, tx *domain.Transaction) error

	// Update persists state or metadata changes to an existing transaction.
	Update(ctx context.Context, tx *domain.Transaction) error

	// FindByID retrieves a transaction by its primary key.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error)

	// FindByReference retrieves a transaction by its unique reference string.
	FindByReference(ctx context.Context, ref string) (*domain.Transaction, error)

	// FindByIDForUpdate retrieves a transaction locked for update within an active SQL transaction.
	FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Transaction, error)

	// FindByWalletID retrieves transactions for a wallet ordered by creation date desc with pagination.
	FindByWalletID(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*domain.Transaction, error)

	// CountByWalletID returns the total count of transactions for a wallet.
	CountByWalletID(ctx context.Context, walletID uuid.UUID) (int64, error)
}
