-- Down migration: drop audit table for carrier_message -> server_message links
DROP INDEX IF EXISTS idx_carrier_message_links_audit_carrier;
DROP TABLE IF EXISTS carrier_message_links_audit;
