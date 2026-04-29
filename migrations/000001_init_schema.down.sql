DROP TRIGGER IF EXISTS trg_subscriptions_updated_at ON subscriptions;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS tracked_repositories;
