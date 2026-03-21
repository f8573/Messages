-- Create message_edits table for storing message edit history
CREATE TABLE message_edits (
  id BIGSERIAL PRIMARY KEY,
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  edited_by UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
  previous_content JSONB NOT NULL,
  new_content JSONB NOT NULL,
  edited_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index for efficient queries when fetching edit history for a message
CREATE INDEX idx_message_edits_message_id_desc ON message_edits(message_id, edited_at DESC);

-- Index for auditing by conversation
CREATE INDEX idx_message_edits_conversation_id ON message_edits(conversation_id, edited_at DESC);
