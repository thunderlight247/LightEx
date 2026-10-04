CREATE TABLE IF NOT EXISTS ledger_entries (

    id UUID PRIMARY KEY,

    transaction_id UUID NOT NULL
        REFERENCES transactions(id)
        ON DELETE CASCADE,

    wallet_id UUID NOT NULL
        REFERENCES wallets(id)
        ON DELETE CASCADE,

    entry_type VARCHAR(10) NOT NULL,

    amount NUMERIC(20,8) NOT NULL,

    currency VARCHAR(10) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_transaction
ON ledger_entries(transaction_id);

CREATE INDEX idx_ledger_wallet
ON ledger_entries(wallet_id);