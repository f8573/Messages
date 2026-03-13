ALTER TABLE miniapp_sessions
  ADD COLUMN IF NOT EXISTS app_id text;

UPDATE miniapp_sessions s
SET app_id = m.manifest->>'app_id'
FROM miniapp_manifests m
WHERE s.manifest_id = m.id
  AND s.app_id IS NULL;

ALTER TABLE miniapp_sessions
  ADD COLUMN IF NOT EXISTS granted_permissions jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS state_version integer NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS created_by uuid;

ALTER TABLE miniapp_sessions
  ALTER COLUMN app_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_miniapp_manifests_app_id
  ON miniapp_manifests ((manifest->>'app_id'));

CREATE UNIQUE INDEX IF NOT EXISTS idx_miniapp_sessions_active_app_conversation
  ON miniapp_sessions (app_id, conversation_id)
  WHERE ended_at IS NULL;
