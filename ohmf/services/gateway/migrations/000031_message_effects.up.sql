-- Table for tracking message effects (confetti, balloons, etc.)
CREATE TABLE message_effects (
  id BIGSERIAL PRIMARY KEY,
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  triggered_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  effect_type TEXT NOT NULL,
  triggered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index for efficient message effect queries
CREATE INDEX idx_message_effects_message
  ON message_effects(message_id, triggered_at DESC);

-- Index for conversation effect history
CREATE INDEX idx_message_effects_conversation
  ON message_effects(conversation_id, triggered_at DESC);

-- Add effect policy to conversations (admins can disable effects)
ALTER TABLE conversations
  ADD COLUMN allow_message_effects BOOLEAN DEFAULT TRUE;
