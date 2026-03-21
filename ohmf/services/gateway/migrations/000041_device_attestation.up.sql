ALTER TABLE devices
  ADD COLUMN IF NOT EXISTS attestation_type TEXT,
  ADD COLUMN IF NOT EXISTS attestation_state TEXT NOT NULL DEFAULT 'UNVERIFIED',
  ADD COLUMN IF NOT EXISTS attestation_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS attested_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS attestation_expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS attestation_public_key_hash TEXT,
  ADD COLUMN IF NOT EXISTS attestation_last_error TEXT;

CREATE INDEX IF NOT EXISTS idx_devices_user_attestation
  ON devices (user_id, attestation_state, attestation_expires_at DESC);

CREATE TABLE IF NOT EXISTS device_attestation_challenges (
  device_id UUID PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform TEXT NOT NULL,
  nonce TEXT NOT NULL,
  public_key_hash TEXT,
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_device_attestation_challenges_user_expires
  ON device_attestation_challenges (user_id, expires_at DESC);
