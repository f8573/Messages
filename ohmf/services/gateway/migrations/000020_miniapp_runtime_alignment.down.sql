DROP INDEX IF EXISTS idx_miniapp_sessions_active_app_conversation;
DROP INDEX IF EXISTS idx_miniapp_manifests_app_id;

ALTER TABLE miniapp_sessions
  DROP COLUMN IF EXISTS created_by,
  DROP COLUMN IF EXISTS state_version,
  DROP COLUMN IF EXISTS granted_permissions,
  DROP COLUMN IF EXISTS app_id;
