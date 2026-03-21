DROP TRIGGER IF EXISTS trg_messages_search_vector ON messages;
DROP FUNCTION IF EXISTS update_messages_search_vector();
DROP INDEX IF EXISTS idx_messages_search_vector;

ALTER TABLE messages
  DROP COLUMN IF EXISTS search_vector;

DROP TABLE IF EXISTS account_exports;
DROP TABLE IF EXISTS device_pairing_sessions;
DROP TABLE IF EXISTS security_audit_events;
