CREATE TABLE IF NOT EXISTS security_audit_events (
  id BIGSERIAL PRIMARY KEY,
  actor_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  target_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_security_audit_events_target_created
  ON security_audit_events (target_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_security_audit_events_type_created
  ON security_audit_events (event_type, created_at DESC);

CREATE TABLE IF NOT EXISTS device_pairing_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  requested_by_device_id UUID NULL REFERENCES devices(id) ON DELETE SET NULL,
  pairing_code TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL DEFAULT 'PENDING',
  expires_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ NULL,
  paired_device_id UUID NULL REFERENCES devices(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_device_pairing_sessions_user_created
  ON device_pairing_sessions (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_device_pairing_sessions_code_status
  ON device_pairing_sessions (pairing_code, status);

CREATE TABLE IF NOT EXISTS account_exports (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'READY',
  export_blob JSONB NOT NULL,
  format TEXT NOT NULL DEFAULT 'application/json',
  download_token TEXT NOT NULL UNIQUE,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '7 days'),
  downloaded_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_account_exports_user_created
  ON account_exports (user_id, created_at DESC);

ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS search_vector tsvector;

CREATE INDEX IF NOT EXISTS idx_messages_search_vector
  ON messages
  USING GIN (search_vector);

UPDATE messages
SET search_vector = to_tsvector(
  'simple',
  trim(
    both ' '
    FROM COALESCE(content->>'text', '') || ' ' || COALESCE(content->>'attachment_id', '')
  )
)
WHERE search_vector IS NULL;

CREATE OR REPLACE FUNCTION update_messages_search_vector() RETURNS trigger AS $$
BEGIN
  NEW.search_vector := to_tsvector(
    'simple',
    trim(
      both ' '
      FROM COALESCE(NEW.content->>'text', '') || ' ' || COALESCE(NEW.content->>'attachment_id', '')
    )
  );
  RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_messages_search_vector ON messages;

CREATE TRIGGER trg_messages_search_vector
BEFORE INSERT OR UPDATE OF content
ON messages
FOR EACH ROW
EXECUTE FUNCTION update_messages_search_vector();
