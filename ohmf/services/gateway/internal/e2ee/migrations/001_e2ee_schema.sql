-- E2EE Database Schema Initialization
-- PostgreSQL 15 compatible
-- Auto-executed by Docker on first startup

-- Device identity keys (long-lived)
CREATE TABLE IF NOT EXISTS device_identity_keys (
  device_id UUID PRIMARY KEY,
  user_id UUID NOT NULL,
  identity_public_key BYTEA NOT NULL,       -- X25519 public key (32 bytes)
  identity_private_key BYTEA NOT NULL,      -- X25519 private key (32 bytes, should be encrypted at rest)
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_device_identity_keys_user_id
  ON device_identity_keys(user_id);

-- Device signed pre-keys (medium-lived, ~4 weeks)
CREATE TABLE IF NOT EXISTS device_signed_prekeys (
  device_id UUID PRIMARY KEY,
  prekey_id BIGINT NOT NULL,
  public_key BYTEA NOT NULL,                -- X25519 public key (32 bytes)
  private_key BYTEA NOT NULL,               -- X25519 private key (32 bytes, encrypted at rest)
  signature BYTEA NOT NULL,                 -- Ed25519 signature (64 bytes)
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- Device one-time pre-keys (ephemeral, consumed after use)
CREATE TABLE IF NOT EXISTS device_one_time_prekeys (
  device_id UUID NOT NULL,
  prekey_id BIGINT NOT NULL,
  public_key BYTEA NOT NULL,                -- X25519 public key (32 bytes)
  private_key BYTEA NOT NULL,               -- X25519 private key (32 bytes, encrypted at rest)
  used_at TIMESTAMP,                        -- NULL until consumed (one-time use)
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
  PRIMARY KEY (device_id, prekey_id)
);

CREATE INDEX IF NOT EXISTS idx_device_one_time_prekeys_used_at
  ON device_one_time_prekeys(used_at)
  WHERE used_at IS NULL;                    -- Find unused keys

-- E2EE Sessions (Double Ratchet state)
CREATE TABLE IF NOT EXISTS sessions (
  user_id UUID NOT NULL,
  contact_user_id UUID NOT NULL,
  contact_device_id UUID NOT NULL,
  root_key_bytes BYTEA NOT NULL,            -- Double Ratchet root key (32 bytes)
  chain_key_bytes BYTEA NOT NULL,           -- Double Ratchet send chain key (32 bytes)
  message_key_index INTEGER NOT NULL DEFAULT 0,  -- Send message counter
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
  PRIMARY KEY (user_id, contact_user_id, contact_device_id)
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id
  ON sessions(user_id);

CREATE INDEX IF NOT EXISTS idx_sessions_contact_user_id
  ON sessions(contact_user_id);

-- Permissions (test database doesn't need strict permissions)
GRANT CONNECT ON DATABASE e2ee_test TO e2ee_test;
GRANT USAGE ON SCHEMA public TO e2ee_test;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO e2ee_test;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO e2ee_test;

-- Confirm initialization
SELECT 'E2EE Schema initialized successfully' as status;
