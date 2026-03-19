ALTER TABLE device_identity_keys
  DROP COLUMN IF EXISTS signing_public_key,
  DROP COLUMN IF EXISTS signing_key_alg,
  DROP COLUMN IF EXISTS agreement_identity_public_key;
