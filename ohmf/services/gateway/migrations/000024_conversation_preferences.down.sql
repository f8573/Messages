DROP INDEX IF EXISTS idx_user_conversation_state_user_closed_updated;

ALTER TABLE user_conversation_state
  DROP COLUMN IF EXISTS is_closed,
  DROP COLUMN IF EXISTS nickname;
