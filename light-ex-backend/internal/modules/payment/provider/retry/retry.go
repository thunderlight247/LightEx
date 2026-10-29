package retry

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"syscall"
	"time"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

var (
	ErrMaxRetriesExceeded = errors.New("maximum retry attempts exceeded")
)

// Policy defines how transient operations should be retried.
type Policy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	IsRetryable  func(err error) bool
}

// DefaultPolicy returns a production-ready retry policy for external network calls.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Multiplier:   2.0,
		IsRetryable:  IsTransientNetworkError,
	}
}

// IsTransientNetworkError identifies network-level timeouts and connection resets.
func IsTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}

	// Do not retry validation errors
	if errors.Is(err, provider.ErrInvalidReference) ||
		errors.Is(err, provider.ErrInvalidAmount) ||
		errors.Is(err, provider.ErrInvalidCurrency) ||
		errors.Is(err, provider.ErrInvalidPhone) {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}

	return false
}

// Do executes an operation with exponential backoff and jitter.
func (p Policy) Do(ctx context.Context, op func(ctx context.Context) error) error {
	attempts := p.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	delay := p.InitialDelay
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}

	multiplier := p.Multiplier
	if multiplier < 1.0 {
		multiplier = 2.0
	}

	isRetryable := p.IsRetryable
	if isRetryable == nil {
		isRetryable = IsTransientNetworkError
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := op(ctx)
		if err == nil {
			return nil
		}

		lastErr = err
		if !isRetryable(err) {
			return err
		}
		if attempt == attempts {
			return errors.Join(ErrMaxRetriesExceeded, lastErr)
		}

		// Calculate backoff with jitter (0.8x to 1.2x)
		jitter := 0.8 + 0.4*rand.Float64()
		sleepDuration := time.Duration(float64(delay) * jitter)
		if p.MaxDelay > 0 && sleepDuration > p.MaxDelay {
			sleepDuration = p.MaxDelay
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDuration):
		}

		delay = time.Duration(float64(delay) * multiplier)
	}

	return errors.Join(ErrMaxRetriesExceeded, lastErr)
}

// RetryableProvider decorates a provider.Provider with retry logic.
type RetryableProvider struct {
	base   provider.Provider
	policy Policy
}

// Wrap decorates a provider.Provider with the specified retry policy.
func Wrap(p provider.Provider, policy Policy) provider.Provider {
	return &RetryableProvider{
		base:   p,
		policy: policy,
	}
}

func (r *RetryableProvider) Name() string {
	return r.base.Name()
}

func (r *RetryableProvider) Collect(ctx context.Context, req provider.Request) (provider.Result, error) {
	var res provider.Result
	err := r.policy.Do(ctx, func(opCtx context.Context) error {
		var opErr error
		res, opErr = r.base.Collect(opCtx, req)
		return opErr
	})
	return res, err
}

func (r *RetryableProvider) Disburse(ctx context.Context, req provider.Request) (provider.Result, error) {
	var res provider.Result
	err := r.policy.Do(ctx, func(opCtx context.Context) error {
		var opErr error
		res, opErr = r.base.Disburse(opCtx, req)
		return opErr
	})
	return res, err
}
