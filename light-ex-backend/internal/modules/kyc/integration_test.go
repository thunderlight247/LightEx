package kyc_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/domain"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/handler"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/repository/postgres"
	"github.com/light-group/light-ex-backend/internal/modules/kyc/service"
	"github.com/light-group/light-ex-backend/internal/platform/config"
	"github.com/light-group/light-ex-backend/internal/platform/database"
	"github.com/light-group/light-ex-backend/internal/platform/middleware"
	jwtservice "github.com/light-group/light-ex-backend/internal/platform/security/jwt"
)

func fixture(t *testing.T) *sql.DB {
	t.Helper()
	_ = godotenv.Load("../../../.env")
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("database configuration unavailable: %v", err)
	}
	admin, err := database.New(cfg.Database)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	schema := "kyc_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.SQL().Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.SQL().Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	db, err := sql.Open("pgx", cfg.Database.DSN()+" search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, file := range []string{"000001_create_users_table.up.sql", "000007_create_kyc_submissions.up.sql", "000008_create_kyc_reviewers.up.sql"} {
		migration, err := os.ReadFile("../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func addUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := db.Exec(`INSERT INTO users (id,email,password_hash,status,created_at,updated_at) VALUES ($1,$2,'test','ACTIVE',now(),now())`, id, id.String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestKYCSubmissionAndReviewPostgres(t *testing.T) {
	db := fixture(t)
	owner, reviewer := addUser(t, db), addUser(t, db)
	if _, err := db.Exec(`UPDATE users SET email_verified=true WHERE id IN ($1,$2)`, owner, reviewer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO kyc_reviewers(user_id) VALUES ($1),($2)`, owner, reviewer); err != nil {
		t.Fatal(err)
	}
	svc := service.New(postgres.New(db))
	ctx := context.Background()
	if _, err := svc.Submit(ctx, owner, " ", "2000-01-01", "GH"); !errors.Is(err, domain.ErrInvalidSubmission) {
		t.Fatalf("invalid name: %v", err)
	}
	if _, err := svc.Submit(ctx, owner, "Test Person", "2999-01-01", "GH"); !errors.Is(err, domain.ErrInvalidSubmission) {
		t.Fatalf("future birth: %v", err)
	}
	s, err := svc.Submit(ctx, owner, " Test Person ", "2000-01-01", "gh")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != domain.Pending || s.Country != "GH" {
		t.Fatalf("submission: %+v", s)
	}
	if _, err := svc.Submit(ctx, owner, "Test Person", "2000-01-01", "GH"); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := svc.Review(ctx, s.ID, owner, domain.Approved); !errors.Is(err, domain.ErrInvalidReview) {
		t.Fatalf("self review: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, decision := range []domain.Status{domain.Approved, domain.Rejected} {
		wg.Add(1)
		go func(decision domain.Status) {
			defer wg.Done()
			_, err := svc.Review(ctx, s.ID, reviewer, decision)
			results <- err
		}(decision)
	}
	wg.Wait()
	close(results)
	succeeded, conflicts := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, repository.ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("review: %v", err)
		}
	}
	if succeeded != 1 || conflicts != 1 {
		t.Fatalf("reviews: success %d conflict %d", succeeded, conflicts)
	}
	final, err := svc.Latest(ctx, owner)
	if err != nil || final.ReviewerID == nil || *final.ReviewerID != reviewer || final.ReviewedAt == nil {
		t.Fatalf("review not persisted: %+v %v", final, err)
	}
	for _, decision := range []domain.Status{domain.Approved, domain.Rejected} {
		t.Run(string(decision), func(t *testing.T) {
			user := addUser(t, db)
			initial, err := svc.Submit(ctx, user, "Test Person", "2000-01-01", "GH")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Review(ctx, initial.ID, reviewer, decision); err != nil {
				t.Fatal(err)
			}
			retry, err := svc.Submit(ctx, user, "Test Person", "2000-01-01", "GH")
			if decision == domain.Approved {
				if !errors.Is(err, repository.ErrConflict) {
					t.Fatalf("approved duplicate: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("resubmit: %v", err)
				}
				latest, err := svc.Latest(ctx, user)
				if err != nil || latest.ID != retry.ID || latest.Status != domain.Pending {
					t.Fatalf("latest resubmission: %+v %v", latest, err)
				}
			}
		})
	}
}

func TestKYCReviewerHTTPAuthorization(t *testing.T) {
	db := fixture(t)
	owner, reviewer, other := addUser(t, db), addUser(t, db), addUser(t, db)
	if _, err := db.Exec(`UPDATE users SET email_verified=true WHERE id IN ($1,$2)`, owner, reviewer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO kyc_reviewers(user_id) VALUES ($1),($2)`, owner, reviewer); err != nil {
		t.Fatal(err)
	}
	svc := service.New(postgres.New(db))
	submission, err := svc.Submit(context.Background(), owner, "Test Person", "2000-01-01", "GH")
	if err != nil {
		t.Fatal(err)
	}
	jwt := jwtservice.New(config.JWTConfig{Secret: "test-secret", Issuer: "test", AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour})
	token := func(id uuid.UUID, refresh bool) string {
		var value string
		var err error
		if refresh {
			value, err = jwt.GenerateRefreshToken(id.String())
		} else {
			value, err = jwt.GenerateAccessToken(id.String())
		}
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	ownerToken, reviewerToken, otherToken, refresh := token(owner, false), token(reviewer, false), token(other, false), token(reviewer, true)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler.New(svc).RegisterRoutes(r.Group("/api/v1"), middleware.Authentication(jwt))
	path := "/api/v1/kyc/submissions/" + submission.ID.String()
	send := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {refresh, 401}, {otherToken, 403}} {
		w := send("GET", path, tc.token, "")
		if w.Code != tc.want {
			t.Fatalf("reviewer auth: got %d want %d: %s", w.Code, tc.want, w.Body.String())
		}
	}
	if w := send("GET", path, reviewerToken, ""); w.Code != 200 {
		t.Fatalf("reviewer lookup: %d %s", w.Code, w.Body.String())
	}
	if w := send("POST", path+"/review", ownerToken, `{"status":"APPROVED"}`); w.Code != 400 {
		t.Fatalf("self review: %d %s", w.Code, w.Body.String())
	}
	if _, err := db.Exec(`UPDATE users SET status='SUSPENDED' WHERE id=$1`, reviewer); err != nil {
		t.Fatal(err)
	}
	if w := send("GET", path, reviewerToken, ""); w.Code != 403 {
		t.Fatalf("suspended reviewer: %d", w.Code)
	}
	if _, err := db.Exec(`UPDATE users SET status='ACTIVE', email_verified=false WHERE id=$1`, reviewer); err != nil {
		t.Fatal(err)
	}
	if w := send("GET", path, reviewerToken, ""); w.Code != 403 {
		t.Fatalf("unverified reviewer: %d", w.Code)
	}
	if _, err := db.Exec(`UPDATE users SET email_verified=true WHERE id=$1`, reviewer); err != nil {
		t.Fatal(err)
	}
	if w := send("POST", path+"/review", reviewerToken, `{"status":"APPROVED"}`); w.Code != 200 {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}
	if w := send("POST", path+"/review", reviewerToken, `{"status":"REJECTED"}`); w.Code != 409 {
		t.Fatalf("terminal review: %d %s", w.Code, w.Body.String())
	}
	if _, err := db.Exec(`DELETE FROM kyc_reviewers WHERE user_id=$1`, reviewer); err != nil {
		t.Fatal(err)
	}
	if w := send("GET", path, reviewerToken, ""); w.Code != 403 {
		t.Fatalf("revoked reviewer: %d", w.Code)
	}
}

type eligibilityRepository struct {
	repository.Repository
	submission *domain.Submission
	err        error
}

func (r eligibilityRepository) LatestByUser(context.Context, uuid.UUID) (*domain.Submission, error) {
	return r.submission, r.err
}

func TestEligibilityFailsClosedOnRepositoryFailures(t *testing.T) {
	user := uuid.New()
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name string
		repo eligibilityRepository
	}{
		{"database error", eligibilityRepository{err: dbErr}},
		{"nil result", eligibilityRepository{}},
		{"wrong user", eligibilityRepository{submission: &domain.Submission{UserID: uuid.New(), Status: domain.Approved}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.New(tc.repo).Eligibility(context.Background(), user)
			if err == nil || result.PaymentEligible {
				t.Fatalf("expected error and no eligibility: %+v, %v", result, err)
			}
		})
	}
	unknown := domain.AssessEligibility(&domain.Submission{Status: "UNEXPECTED"})
	if unknown.PaymentEligible || unknown.KYCState != "UNKNOWN" || unknown.Reason != "KYC_STATUS_INVALID" {
		t.Fatalf("unknown state: %+v", unknown)
	}
}

func TestEligibilityLifecycleHTTP(t *testing.T) {
	db := fixture(t)
	owner, other, reviewer := addUser(t, db), addUser(t, db), addUser(t, db)
	if _, err := db.Exec(`UPDATE users SET email_verified=true WHERE id=$1`, reviewer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO kyc_reviewers(user_id) VALUES ($1)`, reviewer); err != nil {
		t.Fatal(err)
	}
	svc := service.New(postgres.New(db))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	auth := func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(middleware.ContextUserID, token)
		c.Next()
	}
	handler.New(svc).RegisterRoutes(r.Group("/api/v1"), auth)
	get := func(user uuid.UUID, state, reason string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/kyc/eligibility", nil)
		req.Header.Set("Authorization", user.String())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var result domain.Eligibility
		if w.Code != http.StatusOK {
			t.Fatalf("assessment: %d %s", w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.KYCState != state || result.Reason != reason || result.PaymentEligible {
			t.Fatalf("assessment: %+v", result)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/kyc/eligibility", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	get(owner, "NOT_SUBMITTED", "KYC_SUBMISSION_REQUIRED")
	first, err := svc.Submit(context.Background(), owner, "Test Person", "2000-01-01", "GH")
	if err != nil {
		t.Fatal(err)
	}
	get(owner, "PENDING", "KYC_REVIEW_PENDING")
	get(other, "NOT_SUBMITTED", "KYC_SUBMISSION_REQUIRED")
	if _, err := svc.Review(context.Background(), first.ID, reviewer, domain.Rejected); err != nil {
		t.Fatal(err)
	}
	get(owner, "REJECTED", "KYC_SUBMISSION_REJECTED")
	second, err := svc.Submit(context.Background(), owner, "Test Person", "2000-01-01", "GH")
	if err != nil {
		t.Fatal(err)
	}
	get(owner, "PENDING", "KYC_REVIEW_PENDING")
	if _, err := svc.Review(context.Background(), second.ID, reviewer, domain.Approved); err != nil {
		t.Fatal(err)
	}
	get(owner, "APPROVED", "IDENTITY_VERIFICATION_REQUIRED")
}

func TestReviewerGrantAdministrationPostgres(t *testing.T) {
	db := fixture(t)
	svc := service.New(postgres.New(db))
	ctx := context.Background()
	reviewer := addUser(t, db)
	if _, err := db.Exec(`UPDATE users SET email=$1, email_verified=true WHERE id=$2`, "reviewer@example.com", reviewer); err != nil {
		t.Fatal(err)
	}
	isReviewer := func() bool {
		allowed, err := postgres.New(db).IsReviewer(ctx, reviewer)
		if err != nil {
			t.Fatal(err)
		}
		return allowed
	}
	if isReviewer() {
		t.Fatal("user is a reviewer before any grant")
	}
	// Email is normalized, so operator input casing and whitespace do not matter.
	if id, err := svc.GrantReviewer(ctx, "  reviewer@example.com "); err != nil || id != reviewer {
		t.Fatalf("grant: %v %v", id, err)
	}
	if !isReviewer() {
		t.Fatal("grant did not take effect")
	}
	if _, err := svc.GrantReviewer(ctx, "reviewer@example.com"); err != nil {
		t.Fatalf("repeat grant should be a no-op: %v", err)
	}
	if id, err := svc.RevokeReviewer(ctx, "reviewer@example.com"); err != nil || id != reviewer {
		t.Fatalf("revoke: %v %v", id, err)
	}
	if isReviewer() {
		t.Fatal("revoke did not take effect")
	}
	if _, err := svc.RevokeReviewer(ctx, "reviewer@example.com"); err != nil {
		t.Fatalf("repeat revoke should be a no-op: %v", err)
	}
	if _, err := svc.GrantReviewer(ctx, "nobody@example.com"); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("unknown grant: %v", err)
	}
	if _, err := svc.RevokeReviewer(ctx, "nobody@example.com"); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("unknown revoke: %v", err)
	}
}

func TestKYCHTTPUserIsolation(t *testing.T) {
	db := fixture(t)
	owner, other := addUser(t, db), addUser(t, db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	auth := func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(middleware.ContextUserID, token)
		c.Next()
	}
	handler.New(service.New(postgres.New(db))).RegisterRoutes(r.Group("/api/v1"), auth)
	send := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	path := "/api/v1/kyc"
	if w := send("GET", path+"/me", "", ""); w.Code != 401 {
		t.Fatalf("auth: %d", w.Code)
	}
	if w := send("POST", path+"/submissions", owner.String(), `{"legal_name":"","date_of_birth":"2000-01-01","country":"GH"}`); w.Code != 400 {
		t.Fatalf("invalid: %d %s", w.Code, w.Body.String())
	}
	w := send("POST", path+"/submissions", owner.String(), `{"legal_name":"Test Person","date_of_birth":"2000-01-01","country":"GH"}`)
	if w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var submitted domain.Submission
	if err := json.Unmarshal(w.Body.Bytes(), &submitted); err != nil || submitted.Status != domain.Pending {
		t.Fatalf("body: %s %v", w.Body.String(), err)
	}
	if w := send("GET", path+"/me", other.String(), ""); w.Code != 404 {
		t.Fatalf("other user: %d %s", w.Code, w.Body.String())
	}
	if w := send("GET", path+"/me", owner.String(), ""); w.Code != 200 {
		t.Fatalf("owner: %d %s", w.Code, w.Body.String())
	}
	if w := send("POST", path+"/submissions/"+submitted.ID.String()+"/review", owner.String(), `{"status":"APPROVED"}`); w.Code != 401 {
		t.Fatalf("review requires typed access token: %d", w.Code)
	}
}
