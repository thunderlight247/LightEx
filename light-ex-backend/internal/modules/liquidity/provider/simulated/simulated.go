// Package simulated implements fake liquidity only. It never contacts an external service.
package simulated

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/light-group/light-ex-backend/internal/modules/liquidity/domain"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider"
	"github.com/shopspring/decimal"
)

type Scenario string

const (
	Success Scenario = "success"
	Pending Scenario = "pending"
	Failure Scenario = "failure"
	Reject  Scenario = "reject"
	Timeout Scenario = "timeout"
)

type Provider struct {
	mu       sync.Mutex
	orders   map[string]provider.Result
	scenario Scenario
	now      func() time.Time
}

func New(s Scenario) *Provider {
	return &Provider{orders: make(map[string]provider.Result), scenario: s, now: time.Now}
}

// Rates are invented test data, not market prices. Output is exact decimal arithmetic.
func (p *Provider) Quote(ctx context.Context, pair domain.Pair, amount decimal.Decimal) (provider.Quote, error) {
	if err := ctx.Err(); err != nil {
		return provider.Quote{}, err
	}
	if !amount.IsPositive() {
		return provider.Quote{}, domain.ErrInvalidAmount
	}
	if valid, err := domain.NewPair(pair.From, pair.To); err != nil || valid != pair {
		return provider.Quote{}, domain.ErrInvalidPair
	}
	var rate decimal.Decimal
	switch pair {
	case domain.Pair{From: "GHS", To: "USDT"}, domain.Pair{From: "GHS", To: "USDC"}:
		rate = decimal.RequireFromString("0.1")
	case domain.Pair{From: "USDT", To: "GHS"}, domain.Pair{From: "USDC", To: "GHS"}:
		rate = decimal.RequireFromString("10")
	default:
		return provider.Quote{}, provider.ErrRejected
	}
	return provider.Quote{Pair: pair, Amount: amount, Rate: rate, Output: amount.Mul(rate), ExpiresAt: p.now().UTC().Add(time.Minute), Provider: "SIMULATED", Simulated: true}, nil
}

func (p *Provider) Execute(ctx context.Context, q domain.Quote, ref string) (provider.Result, error) {
	if err := ctx.Err(); err != nil {
		return provider.Result{}, err
	}
	if ref == "" || !q.Simulated {
		return provider.Result{}, provider.ErrRejected
	}
	if err := q.Validate(p.now()); err != nil {
		return provider.Result{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.orders[ref]; ok {
		return existing, nil
	}
	if p.scenario == Reject {
		return provider.Result{}, provider.ErrRejected
	}
	status := domain.StatusSucceeded
	switch p.scenario {
	case Success:
	case Pending, Timeout:
		status = domain.StatusPending
	case Failure:
		status = domain.StatusFailed
	default:
		return provider.Result{}, errors.New("invalid simulation scenario")
	}
	result := provider.Result{Reference: ref, Status: status, Simulated: true}
	p.orders[ref] = result
	if p.scenario == Timeout {
		return provider.Result{}, errors.Join(provider.ErrUncertain, context.DeadlineExceeded)
	}
	return result, nil
}

func (p *Provider) Check(ctx context.Context, ref string) (provider.Result, error) {
	if err := ctx.Err(); err != nil {
		return provider.Result{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	result, ok := p.orders[ref]
	if !ok {
		return provider.Result{}, provider.ErrNotFound
	}
	return result, nil
}

// SetStatus simulates a later provider update. Terminal outcomes cannot be changed.
func (p *Provider) SetStatus(ref string, status domain.Status) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.orders[ref]
	if !ok {
		return provider.ErrNotFound
	}
	o := domain.Order{Status: r.Status}
	if err := o.Transition(status, p.now()); err != nil {
		return err
	}
	r.Status = status
	p.orders[ref] = r
	return nil
}
