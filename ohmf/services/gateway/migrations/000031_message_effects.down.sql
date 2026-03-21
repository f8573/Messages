ALTER TABLE conversations
  DROP COLUMN IF EXISTS allow_message_effects;

DROP TABLE IF EXISTS message_effects;
