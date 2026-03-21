-- Add read receipt timestamps and per-message tracking
ALTER TABLE conversation_members
  ADD COLUMN read_at TIMESTAMPTZ,
  ADD COLUMN delivery_at TIMESTAMPTZ;

-- Table for per-message read receipts (allows "read by N people" queries)
CREATE TABLE message_read_receipts (
  id BIGSERIAL PRIMARY KEY,
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  reader_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(message_id, reader_user_id)
);

-- Index for efficient "who read this message" queries
CREATE INDEX idx_message_read_receipts_message
  ON message_read_receipts(message_id, read_at DESC);

-- Index for efficient read status per user queries
CREATE INDEX idx_message_read_receipts_reader
  ON message_read_receipts(reader_user_id, read_at DESC);
