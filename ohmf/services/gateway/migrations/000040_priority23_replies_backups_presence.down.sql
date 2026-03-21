DROP INDEX IF EXISTS idx_messages_reply_to_message;

ALTER TABLE messages
  DROP COLUMN IF EXISTS reply_to_message_id;

DROP INDEX IF EXISTS idx_device_key_backups_user_updated;
DROP INDEX IF EXISTS idx_device_key_backups_user_name_active;
DROP TABLE IF EXISTS device_key_backups;

DROP INDEX IF EXISTS idx_security_audit_events_event_hash;

ALTER TABLE security_audit_events
  DROP COLUMN IF EXISTS event_hash,
  DROP COLUMN IF EXISTS prev_event_hash;
