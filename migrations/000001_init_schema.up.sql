-- tracked_repositories: one row per unique GitHub repo we monitor
CREATE TABLE IF NOT EXISTS tracked_repositories (
    id BIGSERIAL PRIMARY KEY,
    owner VARCHAR(100) NOT NULL,
    name VARCHAR(100) NOT NULL,
    last_seen_tag VARCHAR(255),              -- NULL = never checked
    last_checked_at TIMESTAMPTZ,             -- NULL = never checked
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(owner, name)
);

-- subscriptions: one row per email+repo subscription
CREATE TABLE IF NOT EXISTS subscriptions (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    repo_owner VARCHAR(100) NOT NULL,
    repo_name VARCHAR(100) NOT NULL,
    token VARCHAR(64) NOT NULL UNIQUE,       -- UNIQUE already creates an index
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'active', 'unsubscribed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- FK to tracked_repositories for referential integrity
    CONSTRAINT fk_subscriptions_repo
        FOREIGN KEY (repo_owner, repo_name)
        REFERENCES tracked_repositories(owner, name)
        ON DELETE CASCADE
);

-- Prevent duplicate active/pending subscriptions for same email+repo
CREATE UNIQUE INDEX idx_subscriptions_email_repo_active
    ON subscriptions(email, repo_owner, repo_name)
    WHERE status != 'unsubscribed';

-- Scanner query: find all active subscribers for a repo
CREATE INDEX idx_subscriptions_repo_status
    ON subscriptions(repo_owner, repo_name)
    WHERE status = 'active';

-- List subscriptions by email
CREATE INDEX idx_subscriptions_email_status
    ON subscriptions(email)
    WHERE status = 'active';

-- Auto-update updated_at on row modification
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_subscriptions_updated_at
    BEFORE UPDATE ON subscriptions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
