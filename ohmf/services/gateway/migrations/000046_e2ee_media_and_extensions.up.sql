-- Extension: Media encryption support for E2EE
-- Add encryption metadata columns to attachments table for encrypted media support
ALTER TABLE attachments
  ADD COLUMN IF NOT EXISTS is_encrypted BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS encryption_key_encrypted TEXT,        -- Media key wrapped in message encryption
  ADD COLUMN IF NOT EXISTS media_key_nonce TEXT;                 -- GCM nonce for wrapped key

-- Create index for encrypted attachment filtering
CREATE INDEX IF NOT EXISTS idx_attachments_encrypted
  ON attachments(message_id, is_encrypted)
  WHERE is_encrypted = TRUE;

-- Extension: Message edit E2EE tracking
-- Add E2EE awareness to message edits
ALTER TABLE message_edits
  ADD COLUMN IF NOT EXISTS encrypted_message_id UUID,
  ADD COLUMN IF NOT EXISTS edit_blocked_reason TEXT;             -- e.g., "e2ee_content_immutable"

-- Extension: Message searchability tracking (for encrypted messages)
-- Track which messages are not full-text searchable
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS is_searchable BOOLEAN DEFAULT TRUE;   -- FALSE for encrypted

-- Extension: Mirroring tracking for encrypted messages
-- Track when mirroring is disabled due to encryption
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS mirroring_applied BOOLEAN DEFAULT FALSE;

-- Create analytics table for mirroring disabled events
CREATE TABLE IF NOT EXISTS mirroring_disabled_events (
    id BIGSERIAL PRIMARY KEY,
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,                          -- "e2ee_encryption", "policy_override", etc.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index for analytics
CREATE INDEX IF NOT EXISTS idx_mirroring_disabled_events_reason
  ON mirroring_disabled_events(conversation_id, reason, created_at DESC);

-- Extension: Encrypted search analytics
-- Track searches on encrypted vs plaintext conversations
CREATE TABLE IF NOT EXISTS encrypted_search_sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    query TEXT,
    is_encrypted BOOLEAN,
    result_count INT,
    search_type TEXT,                              -- "server_indexed" or "client_filtered"
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index for analytics
CREATE INDEX IF NOT EXISTS idx_encrypted_search_sessions_user_created
  ON encrypted_search_sessions(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_encrypted_search_sessions_conv_type
  ON encrypted_search_sessions(conversation_id, search_type, created_at DESC);

-- Extension: Group encryption skeleton tables (Phase 7, not yet populated)
-- For future group E2EE implementation
CREATE TABLE IF NOT EXISTS group_encryption_keys (
    group_id UUID NOT NULL,
    generation INT NOT NULL,
    group_key_bytes BYTEA NOT NULL,                -- Group encryption key (MLS framework)
    key_version INT DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, generation),
    FOREIGN KEY (group_id) REFERENCES conversations(id) ON DELETE CASCADE
);

-- Index for group key lookups
CREATE INDEX IF NOT EXISTS idx_group_encryption_keys_latest
  ON group_encryption_keys(group_id, generation DESC);

-- Track group member encryption state (Phase 7, not yet populated)
CREATE TABLE IF NOT EXISTS group_member_keys (
    group_id UUID NOT NULL,
    user_id UUID NOT NULL,
    device_id UUID NOT NULL,
    member_key_path BYTEA,                          -- Ratchet tree path (MLS framework)
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id, device_id),
    FOREIGN KEY (group_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

-- Index for group member lookups
CREATE INDEX IF NOT EXISTS idx_group_member_keys_group
  ON group_member_keys(group_id, created_at DESC);

-- Extension: Account deletion E2EE audit
-- Track when E2EE data is cleaned up for deleted accounts
CREATE TABLE IF NOT EXISTS e2ee_deletion_audit (
    id BIGSERIAL PRIMARY KEY,
    deleted_user_id UUID NOT NULL,
    sessions_deleted INT,
    trust_records_deleted INT,
    group_keys_deleted INT,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for audit trail
CREATE INDEX IF NOT EXISTS idx_e2ee_deletion_audit_deleted_at
  ON e2ee_deletion_audit(deleted_at DESC);

-- Constraint: Ensure search_vector_en is NULL for encrypted messages
-- This is a soft constraint - handled in application logic
-- Trigger to null out search vectors for encrypted messages
CREATE OR REPLACE FUNCTION null_search_vector_for_encrypted() RETURNS trigger AS $$
BEGIN
  IF NEW.is_encrypted = TRUE THEN
    NEW.search_vector_en = NULL;
    NEW.search_text_normalized = NULL;
    NEW.is_searchable = FALSE;
  END IF;
  RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_null_search_for_encrypted ON messages;

CREATE TRIGGER trg_null_search_for_encrypted
BEFORE INSERT OR UPDATE OF is_encrypted
ON messages
FOR EACH ROW
EXECUTE FUNCTION null_search_vector_for_encrypted();
