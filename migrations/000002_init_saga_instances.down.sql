DROP TRIGGER IF EXISTS trg_saga_instances_updated_at ON saga_instances;
DROP INDEX IF EXISTS idx_saga_instances_timeout;
DROP TABLE IF EXISTS saga_instances;
