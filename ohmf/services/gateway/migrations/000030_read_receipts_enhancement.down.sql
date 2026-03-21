DROP TABLE IF EXISTS message_read_receipts;

ALTER TABLE conversation_members
  DROP COLUMN IF EXISTS read_at,
  DROP COLUMN IF EXISTS delivery_at;
