-- P4.1 Event Model: Add event_type enumeration and immutability enforcement
-- This migration defines the 5 event types for session lifecycle tracking:
-- - session_created: Session initialized with participants and permissions
-- - session_joined: New participant joined existing session
-- - storage_updated: AppendEvent call recorded (bridge method invoked)
-- - snapshot_written: State snapshot persisted (SnapshotSession called)
-- - message_projected: Message projected into session (future use)

-- Create enum type for event types
CREATE TYPE miniapp_event_type AS ENUM (
  'session_created',
  'session_joined',
  'storage_updated',
  'snapshot_written',
  'message_projected'
);

-- Alter miniapp_events table to add event_type column
ALTER TABLE miniapp_events ADD COLUMN event_type miniapp_event_type NOT NULL DEFAULT 'storage_updated';

-- Backfill existing events as storage_updated (generic event logging)
UPDATE miniapp_events SET event_type = 'storage_updated' WHERE event_type = 'storage_updated';

-- Create constraint to prevent UPDATE and DELETE on miniapp_events (immutability enforcement)
-- This function prevents any modification to event records once written
CREATE OR REPLACE FUNCTION enforce_miniapp_events_append_only()
RETURNS TRIGGER AS $$
BEGIN
  -- Prevent UPDATE operations
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'miniapp_events table is append-only: UPDATE not allowed';
  END IF;
  -- Prevent DELETE operations
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'miniapp_events table is append-only: DELETE not allowed';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Drop existing trigger if it exists (for idempotency)
DROP TRIGGER IF EXISTS miniapp_events_append_only_trigger ON miniapp_events;

-- Create trigger to enforce append-only constraint
CREATE TRIGGER miniapp_events_append_only_trigger
BEFORE UPDATE OR DELETE ON miniapp_events
FOR EACH ROW
EXECUTE FUNCTION enforce_miniapp_events_append_only();

-- Create index on event_type for querying by event type
CREATE INDEX idx_miniapp_events_type ON miniapp_events(app_session_id, event_type);

-- Create index on actor_user_id for audit queries
CREATE INDEX idx_miniapp_events_actor ON miniapp_events(actor_user_id, created_at);

-- Create index on created_at for time-range queries
CREATE INDEX idx_miniapp_events_created ON miniapp_events(created_at);
