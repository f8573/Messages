ALTER TABLE security_audit_events
  ADD COLUMN IF NOT EXISTS prev_event_hash TEXT,
  ADD COLUMN IF NOT EXISTS event_hash TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_security_audit_events_event_hash
  ON security_audit_events (event_hash)
  WHERE event_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS device_key_backups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
  backup_name TEXT NOT NULL,
  encrypted_blob TEXT NOT NULL,
  wrapping_alg TEXT NOT NULL DEFAULT 'X25519_AES256GCM',
  wrapped_key TEXT,
  recovery_data JSONB NOT NULL DEFAULT '{}'::jsonb,
  attestation_type TEXT,
  attestation_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  backup_hash TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_restored_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_device_key_backups_user_name_active
  ON device_key_backups (user_id, backup_name)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_device_key_backups_user_updated
  ON device_key_backups (user_id, updated_at DESC)
  WHERE deleted_at IS NULL;

ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS reply_to_message_id UUID REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_messages_reply_to_message
  ON messages (reply_to_message_id, created_at ASC)
  WHERE reply_to_message_id IS NOT NULL;
