ALTER TABLE miniapp_sessions
  ADD COLUMN IF NOT EXISTS participant_permissions jsonb NOT NULL DEFAULT '{}'::jsonb;

UPDATE miniapp_sessions
SET participant_permissions = jsonb_build_object(created_by::text, granted_permissions)
WHERE created_by IS NOT NULL
  AND participant_permissions = '{}'::jsonb;
