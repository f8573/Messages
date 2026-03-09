CREATE TABLE IF NOT EXISTS carrier_messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  thread_key TEXT,
  carrier_message_id TEXT,
  direction TEXT NOT NULL,
  transport TEXT NOT NULL,
  text TEXT,
  media_json JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  device_authoritative BOOLEAN NOT NULL DEFAULT true,
  server_message_id UUID,
  raw_payload JSONB
);

CREATE INDEX IF NOT EXISTS idx_carrier_messages_device_thread ON carrier_messages (device_id, thread_key);
