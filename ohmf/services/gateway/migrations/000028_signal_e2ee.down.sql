DROP INDEX IF EXISTS idx_device_identity_keys_user_bundle;

ALTER TABLE device_identity_keys
  DROP COLUMN IF EXISTS fingerprint,
  DROP COLUMN IF EXISTS bundle_version;
