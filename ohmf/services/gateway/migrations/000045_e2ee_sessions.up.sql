-- Create table for E2EE Signal protocol sessions (state per DM)
CREATE TABLE IF NOT EXISTS e2ee_sessions (
    user_id UUID NOT NULL,
    contact_user_id UUID NOT NULL,
    contact_device_id UUID NOT NULL,
    session_key_bytes BYTEA NOT NULL,          -- Serialized Signal session state
    session_key_version INT DEFAULT 1,         -- Allow future key formats
    root_key_bytes BYTEA,                      -- For ratchet state (libsignal managed)
    chain_key_bytes BYTEA,                     -- For ratchet state (libsignal managed)
    message_key_index INT DEFAULT 0,           -- Message counter for ratcheting
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (user_id, contact_user_id, contact_device_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (contact_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (contact_device_id) REFERENCES devices(id) ON DELETE CASCADE
);

-- Create index for efficient session lookups
CREATE INDEX IF NOT EXISTS idx_e2ee_sessions_lookup
  ON e2ee_sessions(user_id, contact_user_id, contact_device_id);

-- Track which device keys are trusted for E2EE (TOFU model)
CREATE TABLE IF NOT EXISTS device_key_trust (
    user_id UUID NOT NULL,
    contact_user_id UUID NOT NULL,
    contact_device_id UUID NOT NULL,
    trust_state TEXT DEFAULT 'TOFU',           -- TOFU, VERIFIED, BLOCKED
    fingerprint TEXT NOT NULL,                 -- SHA256(contact's signing_public_key)
    trusted_device_public_key TEXT,            -- Store for verification
    trust_established_at TIMESTAMPTZ,
    verified_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, contact_user_id, contact_device_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (contact_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (contact_device_id) REFERENCES devices(id) ON DELETE CASCADE
);

-- Create index for trust lookups
CREATE INDEX IF NOT EXISTS idx_device_key_trust_lookup
  ON device_key_trust(user_id, contact_user_id, contact_device_id);

-- Create index for fingerprint verification
CREATE INDEX IF NOT EXISTS idx_device_key_trust_fingerprint
  ON device_key_trust(user_id, fingerprint);

-- Add E2EE state tracking to conversations table
ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS encryption_state TEXT DEFAULT 'PLAINTEXT',  -- PLAINTEXT, PENDING_E2EE, ENCRYPTED, CARRIER_PLAINTEXT
  ADD COLUMN IF NOT EXISTS encryption_ready BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS encryption_setup_initiated_at TIMESTAMPTZ;

-- Create index for encryption state queries
CREATE INDEX IF NOT EXISTS idx_conversations_encryption_state
  ON conversations(id, encryption_state)
  WHERE encryption_state != 'PLAINTEXT';

-- Create table to track E2EE initialization attempts (for debugging/analytics)
CREATE TABLE IF NOT EXISTS e2ee_initialization_log (
    id BIGSERIAL PRIMARY KEY,
    initiator_user_id UUID NOT NULL,
    initiator_device_id UUID NOT NULL,
    recipient_user_id UUID NOT NULL,
    recipient_device_id UUID NOT NULL,
    conversation_id UUID,
    status TEXT,                               -- INITIATED, SESSION_CREATED, FAILED
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (initiator_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (initiator_device_id) REFERENCES devices(id) ON DELETE CASCADE,
    FOREIGN KEY (recipient_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (recipient_device_id) REFERENCES devices(id) ON DELETE CASCADE,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL
);

-- Create index for debugging E2EE issues
CREATE INDEX IF NOT EXISTS idx_e2ee_initialization_log_recipient
  ON e2ee_initialization_log(recipient_user_id, created_at DESC);

-- Add E2EE-related columns to messages table for tracking encryption metadata
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS is_encrypted BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS encryption_scheme TEXT,                     -- OHMF_SIGNAL_V1
  ADD COLUMN IF NOT EXISTS sender_device_id UUID;

-- Create index for encrypted message filtering
CREATE INDEX IF NOT EXISTS idx_messages_encrypted
  ON messages(conversation_id, is_encrypted)
  WHERE is_encrypted = TRUE;

-- Create index for device message filtering
CREATE INDEX IF NOT EXISTS idx_messages_sender_device
  ON messages(conversation_id, sender_device_id)
  WHERE sender_device_id IS NOT NULL;
