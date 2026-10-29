// Package repository defines persistence contracts for the ledger module.
// It contains interfaces only and is independent of any database implementation.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/light-group/light-ex-backend/internal/modules/ledger/domain"
)

// Repository defines persistence operations for ledger entries.
type Repository interface {
	// Create appends a new ledger entry. Entries are never updated or deleted.
	Create(ctx context.Context, entry *domain.Entry) error

	// FindByTransactionID returns all ledger entries posted by a transaction.
	FindByTransactionID(ctx context.Context, transactionID uuid.UUID) ([]*domain.Entry, error)
}
