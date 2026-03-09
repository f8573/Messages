-- Relay jobs table for web-originated carrier relay
CREATE TABLE relay_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  creator_user_id uuid NOT NULL,
  destination jsonb NOT NULL,
  transport_hint text,
  content jsonb NOT NULL,
  status text NOT NULL DEFAULT 'queued',
  executing_device_id uuid,
  result jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz
);

CREATE INDEX idx_relay_jobs_creator ON relay_jobs(creator_user_id);