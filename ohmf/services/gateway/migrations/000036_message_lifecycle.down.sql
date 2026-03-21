DROP INDEX IF EXISTS idx_messages_expires_at;

ALTER TABLE messages
  DROP COLUMN IF EXISTS expires_at;
