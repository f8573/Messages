-- Drop E2EE-related columns from messages table
ALTER TABLE messages
  DROP COLUMN IF EXISTS sender_device_id,
  DROP COLUMN IF EXISTS encryption_scheme,
  DROP COLUMN IF EXISTS is_encrypted;

-- Drop indices from messages table
DROP INDEX IF EXISTS idx_messages_sender_device;
DROP INDEX IF EXISTS idx_messages_encrypted;

-- Drop E2EE initialization log table
DROP TABLE IF EXISTS e2ee_initialization_log;

-- Drop indices for E2EE initialization log
DROP INDEX IF EXISTS idx_e2ee_initialization_log_recipient;

-- Remove E2EE columns from conversations table
ALTER TABLE conversations
  DROP COLUMN IF EXISTS encryption_setup_initiated_at,
  DROP COLUMN IF EXISTS encryption_ready,
  DROP COLUMN IF EXISTS encryption_state;

-- Drop index from conversations table
DROP INDEX IF EXISTS idx_conversations_encryption_state;

-- Drop device key trust table
DROP TABLE IF EXISTS device_key_trust;

-- Drop indices for device key trust
DROP INDEX IF EXISTS idx_device_key_trust_fingerprint;
DROP INDEX IF EXISTS idx_device_key_trust_lookup;

-- Drop E2EE sessions table
DROP TABLE IF EXISTS e2ee_sessions;

-- Drop index for E2EE sessions
DROP INDEX IF EXISTS idx_e2ee_sessions_lookup;
