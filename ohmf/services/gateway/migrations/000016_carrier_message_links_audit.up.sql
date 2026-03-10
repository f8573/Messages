-- Migration: create audit table for carrier_message -> server_message links
CREATE TABLE IF NOT EXISTS carrier_message_links_audit (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  carrier_message_id UUID NOT NULL,
  server_message_id UUID NOT NULL,
  set_at timestamptz NOT NULL DEFAULT now(),
  actor TEXT
);

CREATE INDEX IF NOT EXISTS idx_carrier_message_links_audit_carrier ON carrier_message_links_audit (carrier_message_id);
