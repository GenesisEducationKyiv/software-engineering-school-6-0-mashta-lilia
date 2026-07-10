-- sent_at now marks confirmed delivery, not reservation time: NULL means a
-- send was reserved but never confirmed (failed or in flight), so a redelivery
-- can retry it instead of being treated as a permanent duplicate.
ALTER TABLE sent_notifications ALTER COLUMN sent_at DROP NOT NULL;
ALTER TABLE sent_notifications ALTER COLUMN sent_at DROP DEFAULT;
