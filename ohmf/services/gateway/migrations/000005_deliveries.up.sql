CREATE TABLE IF NOT EXISTS message_deliveries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  recipient_user_id UUID,
  recipient_device_id UUID,
  recipient_phone_e164 TEXT,
  transport TEXT NOT NULL,
  state TEXT NOT NULL,
  provider TEXT,
  submitted_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  failure_code TEXT
);

CREATE INDEX IF NOT EXISTS idx_message_deliveries_message ON message_deliveries(message_id);
