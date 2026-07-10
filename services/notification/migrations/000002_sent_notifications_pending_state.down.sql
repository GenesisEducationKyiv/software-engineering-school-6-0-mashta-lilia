UPDATE sent_notifications SET sent_at = NOW() WHERE sent_at IS NULL;
ALTER TABLE sent_notifications ALTER COLUMN sent_at SET DEFAULT NOW();
ALTER TABLE sent_notifications ALTER COLUMN sent_at SET NOT NULL;
