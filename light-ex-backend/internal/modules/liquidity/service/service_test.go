package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/domain"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider"
	"github.com/light-group/light-ex-backend/internal/modules/liquidity/provider/simulated"
	"github.com/shopspring/decimal"
	"sync"
	"testing"
	"time"
)

func makeQuote(t *testing.T, s *Service, u uuid.UUID) domain.Quote {
	t.Helper()
	q, err := s.Quote(context.Background(), u, domain.Pair{From: "GHS", To: "USDC"}, decimal.NewFromInt(100))
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func TestIdempotencyAndOwnership(t *testing.T) {
	s := New(simulated.New(simulated.Success))
	u := uuid.New()
	q := makeQuote(t, s, u)
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := s.CreateOrder(context.Background(), u, q.ID, "key")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- o.ID
		}()
	}
	wg.Wait()
	close(ids)
	var first uuid.UUID
	for id := range ids {
		if first == uuid.Nil {
			first = id
		}
		if first != id {
			t.Fatal("duplicate created")
		}
	}
	o, err := s.GetOrder(u, first)
	if err != nil || o.Status != domain.StatusSucceeded || o.Settlement != "NOT_VERIFIED" || !o.Simulated {
		t.Fatalf("order %+v %v", o, err)
	}
	if _, err = s.GetOrder(uuid.New(), first); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.CreateOrder(context.Background(), u, q.ID, "different"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	q2 := makeQuote(t, s, u)
	if _, err = s.CreateOrder(context.Background(), u, q2.ID, "key"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	s.now = func() time.Time { return q2.ExpiresAt }
	if _, err = s.CreateOrder(context.Background(), u, q2.ID, "expired"); !errors.Is(err, domain.ErrQuoteExpired) {
		t.Fatal(err)
	}
}
func TestTimeoutReconciliation(t *testing.T) {
	p := simulated.New(simulated.Timeout)
	s := New(p)
	u := uuid.New()
	q := makeQuote(t, s, u)
	o, err := s.CreateOrder(context.Background(), u, q.ID, "timeout")
	if err != nil || o.Status != domain.StatusUncertain {
		t.Fatalf("%+v %v", o, err)
	}
	retry, err := s.CreateOrder(context.Background(), u, q.ID, "timeout")
	if err != nil || retry.ID != o.ID {
		t.Fatal("retry did not reuse order")
	}
	o, err = s.Reconcile(context.Background(), u, o.ID)
	if err != nil || o.Status != domain.StatusPending {
		t.Fatalf("%+v %v", o, err)
	}
	if err = p.SetStatus(o.ProviderReference, domain.StatusSucceeded); err != nil {
		t.Fatal(err)
	}
	o, err = s.Reconcile(context.Background(), u, o.ID)
	if err != nil || o.Status != domain.StatusSucceeded || o.Settlement != "NOT_VERIFIED" {
		t.Fatalf("%+v %v", o, err)
	}
}

type invalidProvider struct{ provider.Provider }

func (invalidProvider) Quote(_ context.Context, p domain.Pair, a decimal.Decimal) (provider.Quote, error) {
	return provider.Quote{Pair: p, Amount: a, Rate: decimal.NewFromInt(1), Output: a, ExpiresAt: time.Now().Add(time.Minute), Provider: "REAL", Simulated: false}, nil
}

type badExecutionProvider struct{ provider.Provider }

func (p badExecutionProvider) Execute(ctx context.Context, q domain.Quote, ref string) (provider.Result, error) {
	return provider.Result{Reference: "different", Status: domain.StatusSucceeded, Simulated: true}, nil
}

func TestInvalidExecutionResponseIsUncertain(t *testing.T) {
	s := New(badExecutionProvider{simulated.New(simulated.Success)})
	u := uuid.New()
	q := makeQuote(t, s, u)
	o, err := s.CreateOrder(context.Background(), u, q.ID, "invalid-result")
	if err != nil || o.Status != domain.StatusUncertain {
		t.Fatalf("%+v %v", o, err)
	}
	_, err = s.Reconcile(context.Background(), u, o.ID)
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("missing provider order: %v", err)
	}
	current, err := s.GetOrder(u, o.ID)
	if err != nil || current.Status != domain.StatusUncertain {
		t.Fatalf("uncertainty lost: %+v %v", current, err)
	}
}

func TestDefinitiveFailures(t *testing.T) {
	for _, scenario := range []simulated.Scenario{simulated.Failure, simulated.Reject} {
		t.Run(string(scenario), func(t *testing.T) {
			s := New(simulated.New(scenario))
			u := uuid.New()
			q := makeQuote(t, s, u)
			o, err := s.CreateOrder(context.Background(), u, q.ID, "failure")
			if err != nil || o.Status != domain.StatusFailed {
				t.Fatalf("%+v %v", o, err)
			}
		})
	}
}

func TestRejectsNonSimulatedProvider(t *testing.T) {
	s := New(invalidProvider{})
	_, err := s.Quote(context.Background(), uuid.New(), domain.Pair{From: "GHS", To: "USDC"}, decimal.NewFromInt(1))
	if !errors.Is(err, ErrProviderResponse) {
		t.Fatal(err)
	}
}
