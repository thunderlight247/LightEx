CREATE TABLE IF NOT EXISTS transactions (

    id UUID PRIMARY KEY,

    wallet_id UUID NOT NULL
        REFERENCES wallets(id)
        ON DELETE CASCADE,

    type VARCHAR(30) NOT NULL,

    amount NUMERIC(20,8) NOT NULL,

    currency VARCHAR(10) NOT NULL,

    reference VARCHAR(120) UNIQUE NOT NULL,

    status VARCHAR(30) NOT NULL,

    description TEXT,

    metadata JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transactions_wallet
ON transactions(wallet_id);

CREATE INDEX idx_transactions_reference
ON transactions(reference);

CREATE INDEX idx_transactions_status
ON transactions(status);