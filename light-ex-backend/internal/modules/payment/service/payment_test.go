package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/domain"
	"github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
)

// memRepo is an in-memory payment repository used to check that service
// transitions are persisted and that rejected transitions write nothing.
type memRepo struct {
	byID    map[uuid.UUID]*domain.Payment
	updates int
}

func newMemRepo() *memRepo {
	return &memRepo{byID: make(map[uuid.UUID]*domain.Payment)}
}

func (m *memRepo) Create(_ context.Context, p *domain.Payment) error {
	cp := *p
	m.byID[p.ID] = &cp
	return nil
}

func (m *memRepo) Update(_ context.Context, p *domain.Payment) error {
	if _, ok := m.byID[p.ID]; !ok {
		return postgres.ErrPaymentNotFound
	}
	cp := *p
	m.byID[p.ID] = &cp
	m.updates++
	return nil
}

func (m *memRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Payment, error) {
	p, ok := m.byID[id]
	if !ok {
		return nil, postgres.ErrPaymentNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *memRepo) FindByReference(_ context.Context, ref string) (*domain.Payment, error) {
	for _, p := range m.byID {
		if p.Reference == ref {
			cp := *p
			return &cp, nil
		}
	}
	return nil, postgres.ErrPaymentNotFound
}

func (m *memRepo) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	return m.FindByID(ctx, id)
}

func (m *memRepo) FindByUserID(_ context.Context, userID uuid.UUID, _, _ int) ([]*domain.Payment, error) {
	var out []*domain.Payment
	for _, p := range m.byID {
		if p.UserID == userID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memRepo) FindByStatusAndUpdatedBefore(_ context.Context, status domain.Status, before time.Time, _ int) ([]*domain.Payment, error) {
	var out []*domain.Payment
	for _, p := range m.byID {
		if p.Status == status && !p.UpdatedAt.After(before) {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func createPending(t *testing.T, svc *Service) *domain.Payment {
	t.Helper()
	p, err := svc.CreatePayment(context.Background(), CreatePaymentInput{
		UserID:    uuid.New(),
		WalletID:  uuid.New(),
		Direction: domain.DirectionDeposit,
		Method:    "CARD",
		Amount:    decimal.NewFromInt(10),
		Currency:  "USD",
	})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	return p
}

func TestPaymentService_HappyPathPersistsEachTransition(t *testing.T) {
	repo := newMemRepo()
	svc := New(repo)
	ctx := context.Background()

	p := createPending(t, svc)

	got, err := svc.MarkProcessing(ctx, p.ID, "PROV-9")
	if err != nil {
		t.Fatalf("MarkProcessing: %v", err)
	}
	if got.Status != domain.StatusProcessing || got.ProviderReference != "PROV-9" {
		t.Fatalf("expected PROCESSING with provider ref, got %s / %q", got.Status, got.ProviderReference)
	}

	got, err = svc.MarkCompleted(ctx, p.ID)
	if err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if got.Status != domain.StatusCompleted {
		t.Fatalf("expected COMPLETED, got %s", got.Status)
	}

	stored, _ := repo.FindByID(ctx, p.ID)
	if stored.Status != domain.StatusCompleted || stored.ProviderReference != "PROV-9" {
		t.Fatalf("expected persisted COMPLETED with provider ref, got %s / %q", stored.Status, stored.ProviderReference)
	}
}

func TestPaymentService_RejectedTransitionWritesNothing(t *testing.T) {
	repo := newMemRepo()
	svc := New(repo)
	ctx := context.Background()

	p := createPending(t, svc)
	updatesBefore := repo.updates

	// PENDING cannot jump straight to COMPLETED.
	if _, err := svc.MarkCompleted(ctx, p.ID); !errors.Is(err, domain.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
	}
	if repo.updates != updatesBefore {
		t.Fatalf("rejected transition must not call Update")
	}
	stored, _ := repo.FindByID(ctx, p.ID)
	if stored.Status != domain.StatusPending {
		t.Fatalf("status must stay PENDING, got %s", stored.Status)
	}
}

func TestPaymentService_TerminalStatesAreFinal(t *testing.T) {
	repo := newMemRepo()
	svc := New(repo)
	ctx := context.Background()

	p := createPending(t, svc)
	if _, err := svc.Cancel(ctx, p.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if _, err := svc.MarkProcessing(ctx, p.ID, ""); !errors.Is(err, domain.ErrInvalidStateTransition) {
		t.Fatalf("CANCELLED -> PROCESSING should be rejected, got %v", err)
	}
	if _, err := svc.MarkFailed(ctx, p.ID); !errors.Is(err, domain.ErrInvalidStateTransition) {
		t.Fatalf("CANCELLED -> FAILED should be rejected, got %v", err)
	}
}

func TestPaymentService_MissingPaymentMapsToServiceError(t *testing.T) {
	svc := New(newMemRepo())

	if _, err := svc.GetPayment(context.Background(), uuid.New()); !errors.Is(err, ErrPaymentNotFound) {
		t.Fatalf("expected ErrPaymentNotFound, got %v", err)
	}
}
