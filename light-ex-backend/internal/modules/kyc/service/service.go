package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/domain"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository"
)

var ErrForbidden = errors.New("KYC reviewer access denied")

type Service struct{ repo repository.Repository }

func New(repo repository.Repository) *Service { return &Service{repo: repo} }
func (s *Service) Submit(ctx context.Context, user uuid.UUID, name, birth, country string) (*domain.Submission, error) {
	submission, err := domain.NewSubmission(user, name, birth, country, time.Now())
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, submission); err != nil {
		return nil, err
	}
	return submission, nil
}
func (s *Service) Latest(ctx context.Context, user uuid.UUID) (*domain.Submission, error) {
	if user == uuid.Nil {
		return nil, domain.ErrInvalidSubmission
	}
	return s.repo.LatestByUser(ctx, user)
}

// Eligibility reports KYC review state without granting payment access.
// Database errors are propagated rather than treated as missing submissions.
func (s *Service) Eligibility(ctx context.Context, user uuid.UUID) (domain.Eligibility, error) {
	if user == uuid.Nil {
		return domain.Eligibility{}, domain.ErrInvalidSubmission
	}
	submission, err := s.repo.LatestByUser(ctx, user)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.AssessEligibility(nil), nil
	}
	if err != nil {
		return domain.Eligibility{}, err
	}
	if submission == nil || submission.UserID != user {
		return domain.Eligibility{}, errors.New("invalid KYC repository response")
	}
	return domain.AssessEligibility(submission), nil
}

// PaymentAllowed is the gate used by deposit and withdrawal routes. It is true only
// when the latest submission is APPROVED. Any error or unexpected state denies access.
func (s *Service) PaymentAllowed(ctx context.Context, user uuid.UUID) (bool, error) {
	assessment, err := s.Eligibility(ctx, user)
	if err != nil {
		return false, err
	}
	return assessment.KYCState == "APPROVED", nil
}

// GrantReviewer and RevokeReviewer are operator actions keyed by account email.
// They are not exposed over HTTP because the API has no administrator role.
func (s *Service) GrantReviewer(ctx context.Context, email string) (uuid.UUID, error) {
	return s.repo.GrantReviewerByEmail(ctx, normalizeEmail(email))
}

func (s *Service) RevokeReviewer(ctx context.Context, email string) (uuid.UUID, error) {
	return s.repo.RevokeReviewerByEmail(ctx, normalizeEmail(email))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Service) authorizeReviewer(ctx context.Context, reviewer uuid.UUID) error {
	if reviewer == uuid.Nil {
		return ErrForbidden
	}
	allowed, err := s.repo.IsReviewer(ctx, reviewer)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) GetForReview(ctx context.Context, id, reviewer uuid.UUID) (*domain.Submission, error) {
	if err := s.authorizeReviewer(ctx, reviewer); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, id)
}

// Review requires a current reviewer grant, an active verified account, and a different subject.
func (s *Service) Review(ctx context.Context, id, reviewer uuid.UUID, decision domain.Status) (*domain.Submission, error) {
	if err := s.authorizeReviewer(ctx, reviewer); err != nil {
		return nil, err
	}
	submission, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateReview(submission.UserID, reviewer, decision); err != nil {
		return nil, err
	}
	return s.repo.Review(ctx, id, reviewer, decision)
}
