ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS avatar_url TEXT,
  ADD COLUMN IF NOT EXISTS encryption_state TEXT NOT NULL DEFAULT 'PLAINTEXT',
  ADD COLUMN IF NOT EXISTS encryption_epoch INT NOT NULL DEFAULT 0;

ALTER TABLE user_conversation_state
  ADD COLUMN IF NOT EXISTS is_archived BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS is_pinned BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS muted_until TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_user_conversation_state_visibility
  ON user_conversation_state (user_id, is_archived, is_pinned, updated_at DESC);

CREATE TABLE IF NOT EXISTS device_identity_keys (
  device_id UUID PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  identity_key_alg TEXT NOT NULL DEFAULT 'X25519',
  identity_public_key TEXT NOT NULL,
  signed_prekey_id BIGINT NOT NULL,
  signed_prekey_public_key TEXT NOT NULL,
  signed_prekey_signature TEXT NOT NULL,
  key_version INT NOT NULL DEFAULT 1,
  trust_level TEXT NOT NULL DEFAULT 'TRUSTED_SELF',
  published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS device_one_time_prekeys (
  device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  prekey_id BIGINT NOT NULL,
  public_key TEXT NOT NULL,
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (device_id, prekey_id)
);

CREATE INDEX IF NOT EXISTS idx_device_one_time_prekeys_available
  ON device_one_time_prekeys (device_id, consumed_at, created_at);

CREATE TABLE IF NOT EXISTS notification_preferences (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  push_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  mute_unknown_senders BOOLEAN NOT NULL DEFAULT FALSE,
  show_previews BOOLEAN NOT NULL DEFAULT TRUE,
  muted_conversation_notifications BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, device_id)
);

ALTER TABLE relay_jobs
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS attested_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS consent_state TEXT NOT NULL DEFAULT 'PENDING_DEVICE',
  ADD COLUMN IF NOT EXISTS required_capability TEXT NOT NULL DEFAULT 'RELAY_EXECUTOR';

UPDATE relay_jobs
SET expires_at = COALESCE(expires_at, created_at + interval '10 minutes');

CREATE INDEX IF NOT EXISTS idx_relay_jobs_status_expires
  ON relay_jobs (status, expires_at);
