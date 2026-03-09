-- Mini-app events table
CREATE TABLE miniapp_events (
  app_session_id uuid NOT NULL,
  event_seq serial NOT NULL,
  actor_user_id uuid,
  event_name text NOT NULL,
  body jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (app_session_id, event_seq)
);

CREATE INDEX idx_miniapp_events_session ON miniapp_events(app_session_id);