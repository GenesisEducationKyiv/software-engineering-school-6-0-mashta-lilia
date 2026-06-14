CREATE TABLE sent_notifications (
    id BIGSERIAL PRIMARY KEY,
    kind VARCHAR(20) NOT NULL CONSTRAINT sent_notifications_kind_check CHECK (kind IN ('confirmation', 'release')),
    -- sha256 hex of the logical key: fixed width, no tokens or emails stored.
    dedup_key VARCHAR(64) NOT NULL UNIQUE,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
