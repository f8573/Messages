DROP INDEX IF EXISTS idx_device_attestation_challenges_user_expires;
DROP TABLE IF EXISTS device_attestation_challenges;

DROP INDEX IF EXISTS idx_devices_user_attestation;

ALTER TABLE devices
  DROP COLUMN IF EXISTS attestation_last_error,
  DROP COLUMN IF EXISTS attestation_public_key_hash,
  DROP COLUMN IF EXISTS attestation_expires_at,
  DROP COLUMN IF EXISTS attested_at,
  DROP COLUMN IF EXISTS attestation_payload,
  DROP COLUMN IF EXISTS attestation_state,
  DROP COLUMN IF EXISTS attestation_type;
