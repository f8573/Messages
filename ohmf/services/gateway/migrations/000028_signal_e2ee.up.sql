ALTER TABLE device_identity_keys
  ADD COLUMN IF NOT EXISTS bundle_version TEXT NOT NULL DEFAULT 'OHMF_LEGACY_V0',
  ADD COLUMN IF NOT EXISTS fingerprint TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_device_identity_keys_user_bundle
  ON device_identity_keys (user_id, bundle_version, updated_at DESC);
