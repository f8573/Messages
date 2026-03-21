-- P4.1 Event Model: Rollback event_type enumeration and immutability enforcement

-- Drop the trigger that enforces append-only constraint
DROP TRIGGER IF EXISTS miniapp_events_append_only_trigger ON miniapp_events;

-- Drop the function that enforces append-only constraint
DROP FUNCTION IF EXISTS enforce_miniapp_events_append_only();

-- Drop indices created for event_type queries
DROP INDEX IF EXISTS idx_miniapp_events_type;
DROP INDEX IF EXISTS idx_miniapp_events_actor;
DROP INDEX IF EXISTS idx_miniapp_events_created;

-- Remove the event_type column from miniapp_events table
ALTER TABLE miniapp_events DROP COLUMN IF EXISTS event_type;

-- Drop the enum type
DROP TYPE IF EXISTS miniapp_event_type;
