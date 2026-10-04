CREATE TABLE IF NOT EXISTS wallets (
    id UUID PRIMARY KEY,

    user_id UUID NOT NULL UNIQUE
        REFERENCES users(id)
        ON DELETE CASCADE,

    available_balance NUMERIC(20,8) NOT NULL DEFAULT 0,

    locked_balance NUMERIC(20,8) NOT NULL DEFAULT 0,

    currency VARCHAR(10) NOT NULL DEFAULT 'USD',

    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_wallets_user_id
ON wallets(user_id);