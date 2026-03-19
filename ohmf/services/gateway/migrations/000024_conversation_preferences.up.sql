ALTER TABLE user_conversation_state
  ADD COLUMN IF NOT EXISTS nickname TEXT,
  ADD COLUMN IF NOT EXISTS is_closed BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_user_conversation_state_user_closed_updated
  ON user_conversation_state (user_id, is_closed, updated_at DESC);
