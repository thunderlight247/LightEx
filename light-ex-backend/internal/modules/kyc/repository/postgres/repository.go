package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/domain"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository"
)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, s *domain.Submission) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO kyc_submissions (id,user_id,legal_name,date_of_birth,country,status,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, s.ID, s.UserID, s.LegalName, s.DateOfBirth, s.Country, s.Status, s.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "kyc_one_active_submission_per_user" {
		return repository.ErrConflict
	}
	return err
}

const columns = `id,user_id,legal_name,to_char(date_of_birth, 'YYYY-MM-DD'),country,status,reviewer_id,reviewed_at,created_at`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (*domain.Submission, error) {
	var s domain.Submission
	err := row.Scan(&s.ID, &s.UserID, &s.LegalName, &s.DateOfBirth, &s.Country, &s.Status, &s.ReviewerID, &s.ReviewedAt, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
func (r *Repository) LatestByUser(ctx context.Context, user uuid.UUID) (*domain.Submission, error) {
	return scan(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM kyc_submissions WHERE user_id=$1 ORDER BY created_at DESC, id DESC LIMIT 1`, user))
}
func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Submission, error) {
	return scan(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM kyc_submissions WHERE id=$1`, id))
}
func (r *Repository) IsReviewer(ctx context.Context, user uuid.UUID) (bool, error) {
	var allowed bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM kyc_reviewers r JOIN users u ON u.id=r.user_id WHERE r.user_id=$1 AND u.status='ACTIVE' AND u.email_verified=true)`, user).Scan(&allowed)
	return allowed, err
}
func (r *Repository) Review(ctx context.Context, id, reviewer uuid.UUID, decision domain.Status) (*domain.Submission, error) {
	s, err := scan(r.db.QueryRowContext(ctx, `UPDATE kyc_submissions SET status=$3,reviewer_id=$2,reviewed_at=NOW() WHERE id=$1 AND status='PENDING' AND user_id<>$2 AND EXISTS (SELECT 1 FROM kyc_reviewers r JOIN users u ON u.id=r.user_id WHERE r.user_id=$2 AND u.status='ACTIVE' AND u.email_verified=true) RETURNING `+columns, id, reviewer, decision))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, repository.ErrConflict
	}
	return s, err
}
