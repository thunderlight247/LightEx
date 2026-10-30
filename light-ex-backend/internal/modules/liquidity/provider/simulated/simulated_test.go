package simulated

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/domain"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func TestScenarios(t *testing.T) {
	for _, tc := range []struct {
		s      Scenario
		status domain.Status
		err    error
	}{{Success, domain.StatusSucceeded, nil}, {Pending, domain.StatusPending, nil}, {Failure, domain.StatusFailed, nil}, {Reject, "", provider.ErrRejected}, {Timeout, "", provider.ErrUncertain}} {
		t.Run(string(tc.s), func(t *testing.T) {
			p := New(tc.s)
			pair := domain.Pair{From: "GHS", To: "USDC"}
			pq, err := p.Quote(context.Background(), pair, decimal.NewFromInt(100))
			if err != nil {
				t.Fatal(err)
			}
			q := domain.Quote{ID: uuid.New(), UserID: uuid.New(), Pair: pq.Pair, Amount: pq.Amount, Rate: pq.Rate, Output: pq.Output, ExpiresAt: pq.ExpiresAt, Provider: pq.Provider, Simulated: pq.Simulated}
			result, err := p.Execute(context.Background(), q, "SIM-1")
			if !errors.Is(err, tc.err) {
				t.Fatalf("%v", err)
			}
			if result.Status != tc.status {
				t.Fatalf("status %s", result.Status)
			}
			if tc.s == Timeout {
				result, err = p.Check(context.Background(), "SIM-1")
				if err != nil || result.Status != domain.StatusPending {
					t.Fatalf("check: %+v %v", result, err)
				}
			}
			if tc.s == Pending {
				if err = p.SetStatus("SIM-1", domain.StatusSucceeded); err != nil {
					t.Fatal(err)
				}
				if err = p.SetStatus("SIM-1", domain.StatusFailed); !errors.Is(err, domain.ErrTransition) {
					t.Fatalf("terminal transition %v", err)
				}
			}
			q.ExpiresAt = time.Now().Add(-time.Second)
			if _, err = p.Execute(context.Background(), q, "new"); !errors.Is(err, domain.ErrQuoteExpired) {
				t.Fatalf("expired: %v", err)
			}
		})
	}
	p := New(Success)
	if _, err := p.Quote(context.Background(), domain.Pair{From: "BTC", To: "GHS"}, decimal.NewFromInt(1)); !errors.Is(err, provider.ErrRejected) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Quote(ctx, domain.Pair{From: "GHS", To: "USDC"}, decimal.NewFromInt(1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
