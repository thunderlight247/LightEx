// Package repository defines persistence contracts for the payment module.
// It contains interfaces only and is independent of any database implementation.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/payment/domain"
)

// Repository defines persistence operations for payments.
type Repository interface {
	// Create persists a new payment.
	Create(ctx context.Context, p *domain.Payment) error

	// Update persists status, provider reference and metadata changes.
	Update(ctx context.Context, p *domain.Payment) error

	// FindByID retrieves a payment by its primary key.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error)

	// FindByReference retrieves a payment by its unique reference.
	FindByReference(ctx context.Context, ref string) (*domain.Payment, error)

	// FindByIDForUpdate retrieves a payment locked for update within an active SQL transaction.
	FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Payment, error)

	// FindByUserID retrieves a user's payments ordered by creation date desc with pagination.
	FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Payment, error)
}
