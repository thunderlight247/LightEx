// Package repository defines persistence contracts for the wallet
// module. It contains interfaces only and is independent of any database
// implementation.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/wallet/domain"
)

// Repository defines the persistence operations required by the wallet module.
//
// Implementations may use PostgreSQL or another storage engine without
// affecting the domain or service layers.
type Repository interface {
	// Create persists a new wallet record.
	Create(ctx context.Context, wallet *domain.Wallet) error

	// Update persists changes to an existing wallet (e.g. balance updates or status changes).
	Update(ctx context.Context, wallet *domain.Wallet) error

	// FindByID returns a wallet by its primary key.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error)

	// FindByUserID returns the wallet associated with a specific user.
	FindByUserID(ctx context.Context, userID uuid.UUID) (*domain.Wallet, error)

	// FindByIDForUpdate returns a wallet locked for update (pessimistic lock) inside a transaction.
	FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Wallet, error)

	// FindByUserIDForUpdate returns a wallet locked for update (pessimistic lock) inside a transaction.
	FindByUserIDForUpdate(ctx context.Context, userID uuid.UUID) (*domain.Wallet, error)

	// ExistsByUserID checks whether a wallet already exists for a user.
	ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error)
}
