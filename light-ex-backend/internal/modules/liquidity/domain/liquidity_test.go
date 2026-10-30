package domain

import (
	"errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func TestPairAndQuote(t *testing.T) {
	p, err := NewPair(" ghs ", "usdc")
	if err != nil || p != (Pair{From: "GHS", To: "USDC"}) {
		t.Fatalf("pair: %+v %v", p, err)
	}
	for _, v := range []Pair{{"GHS", "GHS"}, {"A", "BTC"}, {"U$D", "GHS"}} {
		if _, err := NewPair(v.From, v.To); !errors.Is(err, ErrInvalidPair) {
			t.Fatalf("pair %v: %v", v, err)
		}
	}
	now := time.Now()
	q := Quote{ID: uuid.New(), UserID: uuid.New(), Pair: p, Amount: decimal.NewFromInt(100), Rate: decimal.RequireFromString("0.1"), Output: decimal.NewFromInt(10), Provider: "SIMULATED", ExpiresAt: now.Add(time.Minute)}
	if err := q.Validate(now); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(q.Validate(q.ExpiresAt), ErrQuoteExpired) {
		t.Fatal("expiry must be exclusive")
	}
	q.Output = decimal.NewFromInt(11)
	if !errors.Is(q.Validate(now), ErrInvalidQuote) {
		t.Fatal("invalid output accepted")
	}
}
func TestOrderTransitions(t *testing.T) {
	for _, tc := range []struct {
		from, to Status
		valid    bool
	}{{StatusPending, StatusSucceeded, true}, {StatusUncertain, StatusPending, true}, {StatusPending, StatusFailed, true}, {StatusFailed, StatusSucceeded, false}, {StatusSucceeded, StatusPending, false}, {StatusPending, "BOGUS", false}} {
		o := Order{Status: tc.from}
		err := o.Transition(tc.to, time.Now())
		if (err == nil) != tc.valid {
			t.Fatalf("%s -> %s: %v", tc.from, tc.to, err)
		}
	}
}
