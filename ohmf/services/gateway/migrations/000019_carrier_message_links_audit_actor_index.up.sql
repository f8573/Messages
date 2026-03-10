-- Create an index on the actor column to speed audit queries
CREATE INDEX IF NOT EXISTS idx_carrier_message_links_audit_actor ON carrier_message_links_audit (actor);
