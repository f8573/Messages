ALTER TABLE conversation_members
ADD COLUMN IF NOT EXISTS last_read_at TIMESTAMPTZ;
