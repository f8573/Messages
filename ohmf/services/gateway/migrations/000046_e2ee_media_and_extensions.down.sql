-- Rollback: Media encryption support
ALTER TABLE attachments
  DROP COLUMN IF EXISTS is_encrypted,
  DROP COLUMN IF EXISTS encryption_key_encrypted,
  DROP COLUMN IF EXISTS media_key_nonce;

DROP INDEX IF EXISTS idx_attachments_encrypted;

-- Rollback: Message edit E2EE tracking
ALTER TABLE message_edits
  DROP COLUMN IF EXISTS encrypted_message_id,
  DROP COLUMN IF EXISTS edit_blocked_reason;

-- Rollback: Message searchability tracking
ALTER TABLE messages
  DROP COLUMN IF EXISTS is_searchable;

-- Rollback: Mirroring tracking
ALTER TABLE messages
  DROP COLUMN IF EXISTS mirroring_applied;

DROP TABLE IF EXISTS mirroring_disabled_events;

-- Rollback: Encrypted search analytics
DROP TABLE IF EXISTS encrypted_search_sessions;

-- Rollback: Group encryption skeleton tables
DROP TABLE IF EXISTS group_member_keys;
DROP TABLE IF EXISTS group_encryption_keys;

-- Rollback: Account deletion audit
DROP TABLE IF EXISTS e2ee_deletion_audit;

-- Rollback: Search vector nullification trigger
DROP TRIGGER IF EXISTS trg_null_search_for_encrypted ON messages;
DROP FUNCTION IF EXISTS null_search_vector_for_encrypted();
