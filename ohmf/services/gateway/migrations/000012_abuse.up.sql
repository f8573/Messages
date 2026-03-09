-- Abuse events and simple scoring
CREATE TABLE abuse_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_user_id uuid NULL,
  target_user_id uuid NULL,
  event_type text NOT NULL,
  details jsonb,
  ip_address text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_abuse_events_target ON abuse_events(target_user_id);
CREATE INDEX idx_abuse_events_actor ON abuse_events(actor_user_id);
CREATE INDEX idx_abuse_events_type ON abuse_events(event_type);

-- A lightweight table for aggregated scores (optional, updated by worker)
CREATE TABLE IF NOT EXISTS abuse_scores (
  user_id uuid PRIMARY KEY,
  score int NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);
