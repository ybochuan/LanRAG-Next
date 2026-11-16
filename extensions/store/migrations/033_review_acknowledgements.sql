-- Review acknowledgement: a review alert is an in-app message, so it needs
-- a durable read state separate from delivery. delivered_at records that the
-- local callback accepted the reminder; acknowledged_at records that a person
-- has seen it in the message centre. They are deliberately different facts.
ALTER TABLE ext_review_alerts ADD COLUMN acknowledged_at TEXT;

-- The message centre reads unread first, newest first. The partial index keeps
-- that query off the full table while leaving resolved alerts out of it.
CREATE INDEX ext_review_alerts_unread
    ON ext_review_alerts(tenant_id,knowledge_base_id,detected_at)
    WHERE status='open' AND acknowledged_at IS NULL;
