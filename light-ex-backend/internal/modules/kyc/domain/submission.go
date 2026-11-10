package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Status string

const (
	Pending  Status = "PENDING"
	Approved Status = "APPROVED"
	Rejected Status = "REJECTED"
)

var (
	ErrInvalidSubmission = errors.New("invalid KYC submission")
	ErrInvalidReview     = errors.New("invalid KYC review")
)

// Eligibility is an advisory assessment, not an authorization to move funds.
type Eligibility struct {
	KYCState        string `json:"kyc_state"`
	PaymentEligible bool   `json:"payment_eligible"`
	Reason          string `json:"reason"`
}

// AssessEligibility fails closed: manual review alone is not identity verification.
func AssessEligibility(submission *Submission) Eligibility {
	result := Eligibility{KYCState: "NOT_SUBMITTED", Reason: "KYC_SUBMISSION_REQUIRED"}
	if submission == nil {
		return result
	}
	switch submission.Status {
	case Pending:
		result.KYCState, result.Reason = "PENDING", "KYC_REVIEW_PENDING"
	case Rejected:
		result.KYCState, result.Reason = "REJECTED", "KYC_SUBMISSION_REJECTED"
	case Approved:
		result.KYCState, result.Reason = "APPROVED", "IDENTITY_VERIFICATION_REQUIRED"
	default:
		result.KYCState, result.Reason = "UNKNOWN", "KYC_STATUS_INVALID"
	}
	return result
}

// Submission records self-reported information, not evidence of verified identity.
type Submission struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"-"`
	LegalName   string     `json:"legal_name"`
	DateOfBirth string     `json:"date_of_birth"`
	Country     string     `json:"country"`
	Status      Status     `json:"status"`
	ReviewerID  *uuid.UUID `json:"-"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func NewSubmission(user uuid.UUID, name, birth, country string, now time.Time) (*Submission, error) {
	name = strings.TrimSpace(name)
	country = strings.ToUpper(strings.TrimSpace(country))
	dob, err := time.Parse("2006-01-02", birth)
	today := now.UTC().Format("2006-01-02")
	if user == uuid.Nil || name == "" || utf8.RuneCountInString(name) > 200 || err != nil || dob.Year() < 1900 || birth > today || len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return nil, ErrInvalidSubmission
	}
	return &Submission{ID: uuid.New(), UserID: user, LegalName: name, DateOfBirth: birth, Country: country, Status: Pending, CreatedAt: now.UTC()}, nil
}

func ValidateReview(user, reviewer uuid.UUID, decision Status) error {
	if user == uuid.Nil || reviewer == uuid.Nil || user == reviewer || (decision != Approved && decision != Rejected) {
		return ErrInvalidReview
	}
	return nil
}
