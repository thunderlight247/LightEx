CREATE TABLE provider_callback_events (
    provider TEXT NOT NULL,
    reference TEXT NOT NULL,
    status TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, reference, status)
);
