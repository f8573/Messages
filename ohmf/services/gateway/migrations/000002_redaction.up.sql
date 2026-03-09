ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS redacted_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS visibility_state TEXT NOT NULL DEFAULT 'ACTIVE';

-- Ensure existing rows have a valid visibility_state
UPDATE messages SET visibility_state = 'ACTIVE' WHERE visibility_state IS NULL;
