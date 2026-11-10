package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/domain"
)

var (
	ErrNotFound = errors.New("KYC submission not found")
	ErrConflict = errors.New("KYC submission already exists or is no longer pending")
)

type Repository interface {
	Create(context.Context, *domain.Submission) error
	LatestByUser(context.Context, uuid.UUID) (*domain.Submission, error)
	FindByID(context.Context, uuid.UUID) (*domain.Submission, error)
	Review(context.Context, uuid.UUID, uuid.UUID, domain.Status) (*domain.Submission, error)
	IsReviewer(context.Context, uuid.UUID) (bool, error)
}
