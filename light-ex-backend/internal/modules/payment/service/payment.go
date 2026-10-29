package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	"github.com/light-group/light-ex-backend/internal/modules/payment/repository"
)

var (
	ErrPaymentNotFound = errors.New("payment not found")
	ErrWalletNotFound  = errors.New("wallet not found")
)

// Service manages the payment lifecycle. It does not move balances;
// deposit and withdrawal orchestration (Phase 4.5/4.6) coordinate those.
type Service struct {
	repo repository.Repository
}

// New creates a payment service.
func New(repo repository.Repository) *Service {
	return &Service{repo: repo}
}

// CreatePaymentInput contains the fields needed to create a payment.
type CreatePaymentInput struct {
	UserID      uuid.UUID
	WalletID    uuid.UUID
	Direction   domain.Direction
	Method      string
	Amount      decimal.Decimal
	Currency    string
	Reference   string
	Description string
	Metadata    map[string]any
}

// CreatePayment records a new payment in PENDING status.
func (s *Service) CreatePayment(ctx context.Context, in CreatePaymentInput) (*domain.Payment, error) {
	p, err := domain.NewPayment(
		in.UserID,
		in.WalletID,
		in.Direction,
		in.Method,
		in.Amount,
		in.Currency,
		in.Reference,
		in.Description,
		in.Metadata,
	)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// GetPayment returns a payment by ID.
func (s *Service) GetPayment(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return p, nil
}

// ListPaymentsByUser returns a user's payments, newest first.
func (s *Service) ListPaymentsByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Payment, error) {
	return s.repo.FindByUserID(ctx, userID, limit, offset)
}

// MarkProcessing moves a PENDING payment to PROCESSING.
func (s *Service) MarkProcessing(ctx context.Context, id uuid.UUID, providerReference string) (*domain.Payment, error) {
	return s.transition(ctx, id, domain.StatusProcessing, providerReference)
}

// MarkCompleted moves a PROCESSING payment to COMPLETED.
func (s *Service) MarkCompleted(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	return s.transition(ctx, id, domain.StatusCompleted, "")
}

// MarkFailed moves a PENDING or PROCESSING payment to FAILED.
func (s *Service) MarkFailed(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	return s.transition(ctx, id, domain.StatusFailed, "")
}

// Cancel moves a PENDING payment to CANCELLED.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	return s.transition(ctx, id, domain.StatusCancelled, "")
}

func (s *Service) transition(ctx context.Context, id uuid.UUID, to domain.Status, providerReference string) (*domain.Payment, error) {
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}

	if err := p.TransitionTo(to); err != nil {
		return nil, err
	}
	if providerReference != "" {
		p.SetProviderReference(providerReference)
	}

	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}
