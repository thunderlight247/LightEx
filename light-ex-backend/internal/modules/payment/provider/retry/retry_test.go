package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

type mockProvider struct {
	collectCalls  int32
	disburseCalls int32
	collectFn     func(ctx context.Context, req provider.Request) (provider.Result, error)
	disburseFn    func(ctx context.Context, req provider.Request) (provider.Result, error)
}

func (m *mockProvider) Name() string { return "MOCK" }
func (m *mockProvider) Collect(ctx context.Context, req provider.Request) (provider.Result, error) {
	atomic.AddInt32(&m.collectCalls, 1)
	if m.collectFn != nil {
		return m.collectFn(ctx, req)
	}
	return provider.Result{ProviderReference: "ref-1", Status: provider.StatusPending}, nil
}
func (m *mockProvider) Disburse(ctx context.Context, req provider.Request) (provider.Result, error) {
	atomic.AddInt32(&m.disburseCalls, 1)
	if m.disburseFn != nil {
		return m.disburseFn(ctx, req)
	}
	return provider.Result{ProviderReference: "ref-2", Status: provider.StatusPending}, nil
}

func TestPolicy_SuccessOnFirstAttempt(t *testing.T) {
	policy := Policy{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
	}

	var calls int
	err := policy.Do(context.Background(), func(ctx context.Context) error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestPolicy_SuccessAfterTransientError(t *testing.T) {
	var calls int
	errTransient := errors.New("temporary timeout")

	policy := Policy{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		IsRetryable: func(err error) bool {
			return errors.Is(err, errTransient)
		},
	}

	err := policy.Do(context.Background(), func(ctx context.Context) error {
		calls++
		if calls < 2 {
			return errTransient
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestPolicy_FailsImmediatelyOnNonRetryable(t *testing.T) {
	var calls int
	errFatal := errors.New("fatal 400 bad request")

	policy := Policy{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		IsRetryable: func(err error) bool {
			return false
		},
	}

	err := policy.Do(context.Background(), func(ctx context.Context) error {
		calls++
		return errFatal
	})

	if !errors.Is(err, errFatal) {
		t.Fatalf("expected fatal error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestPolicy_ExceedsMaxAttempts(t *testing.T) {
	var calls int
	errTransient := errors.New("temporary error")

	policy := Policy{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		IsRetryable: func(err error) bool {
			return true
		},
	}

	err := policy.Do(context.Background(), func(ctx context.Context) error {
		calls++
		return errTransient
	})

	if !errors.Is(err, ErrMaxRetriesExceeded) {
		t.Fatalf("expected ErrMaxRetriesExceeded, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestPolicy_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	policy := DefaultPolicy()
	err := policy.Do(ctx, func(ctx context.Context) error {
		return errors.New("failed")
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRetryableProvider_Wrap(t *testing.T) {
	var calls int32
	mock := &mockProvider{
		collectFn: func(ctx context.Context, req provider.Request) (provider.Result, error) {
			val := atomic.AddInt32(&calls, 1)
			if val == 1 {
				return provider.Result{}, errors.New("network glitch")
			}
			return provider.Result{ProviderReference: "prov-123", Status: provider.StatusPending}, nil
		},
	}

	policy := Policy{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		IsRetryable: func(err error) bool {
			return true
		},
	}

	wrapped := Wrap(mock, policy)
	if wrapped.Name() != "MOCK" {
		t.Errorf("expected MOCK, got %s", wrapped.Name())
	}

	res, err := wrapped.Collect(context.Background(), provider.Request{
		Reference: "REF-1",
		Amount:    decimal.NewFromInt(10),
		Currency:  "GHS",
		Phone:     "0240000000",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ProviderReference != "prov-123" {
		t.Errorf("expected prov-123, got %s", res.ProviderReference)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected 2 calls, got %d", atomic.LoadInt32(&calls))
	}
}
