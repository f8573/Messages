DROP INDEX IF EXISTS idx_conversation_bans_active;
DROP TABLE IF EXISTS conversation_bans;

DROP INDEX IF EXISTS idx_conversation_invites_active;
DROP TABLE IF EXISTS conversation_invites;

DROP INDEX IF EXISTS idx_conversations_expires_at;

ALTER TABLE conversations
  DROP COLUMN IF EXISTS theme,
  DROP COLUMN IF EXISTS retention_seconds,
  DROP COLUMN IF EXISTS expires_at,
  DROP COLUMN IF EXISTS settings_version,
  DROP COLUMN IF EXISTS settings_updated_at;
