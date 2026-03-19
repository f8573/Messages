DROP TABLE IF EXISTS user_device_cursors;
DROP TABLE IF EXISTS user_conversation_state;
DROP TABLE IF EXISTS user_inbox_events;
DROP TABLE IF EXISTS domain_events;
ALTER TABLE conversation_members DROP COLUMN IF EXISTS last_delivered_server_order;
