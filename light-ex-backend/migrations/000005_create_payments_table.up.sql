CREATE TABLE IF NOT EXISTS payments (

    id UUID PRIMARY KEY,

    user_id UUID NOT NULL
        REFERENCES users(id)
        ON DELETE CASCADE,

    wallet_id UUID NOT NULL
        REFERENCES wallets(id)
        ON DELETE CASCADE,

    direction VARCHAR(20) NOT NULL,

    method VARCHAR(30) NOT NULL,

    amount NUMERIC(20,8) NOT NULL,

    currency VARCHAR(10) NOT NULL,

    status VARCHAR(30) NOT NULL,

    reference VARCHAR(120) UNIQUE NOT NULL,

    provider_reference VARCHAR(120),

    description TEXT,

    metadata JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payments_user
ON payments(user_id);

CREATE INDEX idx_payments_wallet
ON payments(wallet_id);

CREATE INDEX idx_payments_status
ON payments(status);

CREATE INDEX idx_payments_provider_reference
ON payments(provider_reference);