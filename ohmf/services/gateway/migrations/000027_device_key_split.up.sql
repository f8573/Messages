ALTER TABLE device_identity_keys
  ADD COLUMN IF NOT EXISTS agreement_identity_public_key TEXT,
  ADD COLUMN IF NOT EXISTS signing_key_alg TEXT NOT NULL DEFAULT 'ECDSA_P256_SHA256',
  ADD COLUMN IF NOT EXISTS signing_public_key TEXT;

UPDATE device_identity_keys
SET agreement_identity_public_key = COALESCE(NULLIF(agreement_identity_public_key, ''), identity_public_key),
    signing_public_key = COALESCE(NULLIF(signing_public_key, ''), identity_public_key)
WHERE agreement_identity_public_key IS NULL
   OR signing_public_key IS NULL;

ALTER TABLE device_identity_keys
  ALTER COLUMN agreement_identity_public_key SET NOT NULL,
  ALTER COLUMN signing_public_key SET NOT NULL;
