CREATE TABLE kyc_submissions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    legal_name VARCHAR(200) NOT NULL,
    date_of_birth DATE NOT NULL,
    country CHAR(2) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    reviewer_id UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT kyc_review_consistency CHECK (
        (status = 'PENDING' AND reviewer_id IS NULL AND reviewed_at IS NULL) OR
        (status IN ('APPROVED', 'REJECTED') AND reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL AND reviewer_id <> user_id)
    )
);

CREATE UNIQUE INDEX kyc_one_active_submission_per_user
ON kyc_submissions (user_id) WHERE status IN ('PENDING', 'APPROVED');
CREATE INDEX kyc_submissions_user_created ON kyc_submissions (user_id, created_at DESC);
