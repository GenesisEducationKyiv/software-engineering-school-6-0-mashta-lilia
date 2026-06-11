CREATE TABLE sent_notifications (
    id BIGSERIAL PRIMARY KEY,
    kind VARCHAR(20) NOT NULL,
    recipient VARCHAR(255) NOT NULL,
    -- sha256 hex of the logical key: fixed width, no tokens or emails stored.
    dedup_key VARCHAR(64) NOT NULL UNIQUE,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
