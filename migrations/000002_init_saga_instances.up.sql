-- saga_instances: one row per orchestrated saga, durable across restarts so a
-- crash mid-flight can be recovered/compensated by the timeout reaper.
CREATE TABLE IF NOT EXISTS saga_instances (
    id VARCHAR(64) PRIMARY KEY,
    saga_type VARCHAR(50) NOT NULL,
    state VARCHAR(30) NOT NULL
        CHECK (state IN ('awaiting_confirmation', 'completed', 'compensating', 'failed')),
    data JSONB NOT NULL,
    timeout_at TIMESTAMPTZ NOT NULL,
    compensated_at TIMESTAMPTZ,         -- NULL until compensation runs
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Reaper lookup: in-flight sagas past their deadline.
CREATE INDEX idx_saga_instances_timeout
    ON saga_instances(timeout_at)
    WHERE state = 'awaiting_confirmation';

CREATE TRIGGER trg_saga_instances_updated_at
    BEFORE UPDATE ON saga_instances
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
