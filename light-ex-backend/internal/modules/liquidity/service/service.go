// Package service orchestrates simulated liquidity without accessing financial modules.
package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/domain"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider"
	"github.com/shopspring/decimal"
)

var (
	ErrNotFound         = errors.New("liquidity record not found")
	ErrConflict         = errors.New("idempotency key or quote already used")
	ErrInvalidKey       = errors.New("idempotency key must be between 1 and 120 characters")
	ErrProviderResponse = errors.New("invalid or non-simulated provider response")
)

// Service is intentionally restricted to simulations and in-memory persistence.
// Do not use it for a real provider: it does not offer durable idempotency.
type Service struct {
	mu         sync.Mutex
	provider   provider.Provider
	quotes     map[uuid.UUID]domain.Quote
	orders     map[uuid.UUID]domain.Order
	keys       map[string]uuid.UUID
	usedQuotes map[uuid.UUID]uuid.UUID
	now        func() time.Time
}

func New(p provider.Provider) *Service {
	return &Service{provider: p, quotes: make(map[uuid.UUID]domain.Quote), orders: make(map[uuid.UUID]domain.Order), keys: make(map[string]uuid.UUID), usedQuotes: make(map[uuid.UUID]uuid.UUID), now: time.Now}
}

func (s *Service) Quote(ctx context.Context, user uuid.UUID, pair domain.Pair, amount decimal.Decimal) (domain.Quote, error) {
	if user == uuid.Nil {
		return domain.Quote{}, ErrNotFound
	}
	valid, err := domain.NewPair(pair.From, pair.To)
	if err != nil {
		return domain.Quote{}, err
	}
	if !amount.IsPositive() {
		return domain.Quote{}, domain.ErrInvalidAmount
	}
	p, err := s.provider.Quote(ctx, valid, amount)
	if err != nil {
		return domain.Quote{}, err
	}
	q := domain.Quote{ID: uuid.New(), UserID: user, Pair: p.Pair, Amount: p.Amount, Rate: p.Rate, Output: p.Output, ExpiresAt: p.ExpiresAt, Provider: p.Provider, Simulated: p.Simulated}
	if !q.Simulated || q.Provider != "SIMULATED" || q.Pair != valid || !q.Amount.Equal(amount) {
		return domain.Quote{}, ErrProviderResponse
	}
	if err := q.Validate(s.now()); err != nil {
		return domain.Quote{}, err
	}
	s.mu.Lock()
	s.quotes[q.ID] = q
	s.mu.Unlock()
	slog.Info("simulated liquidity quote", "quote_id", q.ID, "user_id", user)
	return q, nil
}

func (s *Service) CreateOrder(ctx context.Context, user, quoteID uuid.UUID, key string) (domain.Order, error) {
	key = strings.TrimSpace(key)
	if user == uuid.Nil || len(key) == 0 || len(key) > 120 {
		return domain.Order{}, ErrInvalidKey
	}
	// The mutex serializes all submissions in this single-process simulator.
	// It is deliberately not a substitute for a durable production idempotency store.
	s.mu.Lock()
	defer s.mu.Unlock()
	keyID := user.String() + ":" + key
	if id, ok := s.keys[keyID]; ok {
		o := s.orders[id]
		if o.QuoteID != quoteID {
			return domain.Order{}, ErrConflict
		}
		return o, nil
	}
	q, ok := s.quotes[quoteID]
	if !ok || q.UserID != user {
		return domain.Order{}, ErrNotFound
	}
	if _, ok := s.usedQuotes[quoteID]; ok {
		return domain.Order{}, ErrConflict
	}
	if err := q.Validate(s.now()); err != nil {
		return domain.Order{}, err
	}
	now := s.now().UTC()
	o := domain.Order{ID: uuid.New(), UserID: user, QuoteID: quoteID, IdempotencyKey: key, Status: domain.StatusPending, Settlement: "NOT_VERIFIED", Simulated: true, CreatedAt: now, UpdatedAt: now}
	// Reserve before invoking the provider; all ambiguous outcomes retain this ID.
	o.ProviderReference = "SIM-" + o.ID.String()
	s.orders[o.ID] = o
	s.keys[keyID] = o.ID
	s.usedQuotes[quoteID] = o.ID
	result, err := s.provider.Execute(ctx, q, o.ProviderReference)
	switch {
	case errors.Is(err, provider.ErrRejected):
		_ = o.Transition(domain.StatusFailed, s.now())
	case err != nil:
		_ = o.Transition(domain.StatusUncertain, s.now())
	case validateResult(result, o.ProviderReference) != nil:
		_ = o.Transition(domain.StatusUncertain, s.now())
	default:
		_ = o.Transition(result.Status, s.now())
	}
	s.orders[o.ID] = o
	slog.Info("simulated liquidity order", "order_id", o.ID, "quote_id", quoteID, "provider_status", o.Status)
	return o, nil
}

func validateResult(r provider.Result, ref string) error {
	if !r.Simulated || r.Reference != ref || (r.Status != domain.StatusPending && r.Status != domain.StatusSucceeded && r.Status != domain.StatusFailed) {
		return ErrProviderResponse
	}
	return nil
}

func (s *Service) GetOrder(user, id uuid.UUID) (domain.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok || o.UserID != user {
		return domain.Order{}, ErrNotFound
	}
	return o, nil
}

// Reconcile checks the same provider reference. A missing or invalid response is
// never treated as a failed execution and never initiates a replacement order.
func (s *Service) Reconcile(ctx context.Context, user, id uuid.UUID) (domain.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok || o.UserID != user {
		return domain.Order{}, ErrNotFound
	}
	if o.Status == domain.StatusSucceeded || o.Status == domain.StatusFailed {
		return o, nil
	}
	r, err := s.provider.Check(ctx, o.ProviderReference)
	if err != nil {
		return o, err
	}
	if err := validateResult(r, o.ProviderReference); err != nil {
		return o, err
	}
	if err := o.Transition(r.Status, s.now()); err != nil {
		return o, err
	}
	s.orders[id] = o
	slog.Info("simulated liquidity reconciliation", "order_id", id, "provider_status", o.Status)
	return o, nil
}
