package provider

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func validRequest() Request {
	return Request{
		Reference: "MM-DEP-1",
		Amount:    decimal.NewFromInt(10),
		Currency:  "GHS",
		Phone:     "+233240000000",
	}
}

func TestValidate_AcceptsValidRequest(t *testing.T) {
	if err := Validate(validRequest()); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}
}

func TestValidate_RejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Request)
		wantErr error
	}{
		{"empty reference", func(r *Request) { r.Reference = "  " }, ErrInvalidReference},
		{"zero amount", func(r *Request) { r.Amount = decimal.Zero }, ErrInvalidAmount},
		{"negative amount", func(r *Request) { r.Amount = decimal.NewFromInt(-1) }, ErrInvalidAmount},
		{"short currency", func(r *Request) { r.Currency = "GH" }, ErrInvalidCurrency},
		{"empty phone", func(r *Request) { r.Phone = "" }, ErrInvalidPhone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest()
			tt.mutate(&req)
			if err := Validate(req); !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}
