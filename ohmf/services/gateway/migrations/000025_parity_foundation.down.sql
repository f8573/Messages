DROP INDEX IF EXISTS idx_relay_jobs_status_expires;

ALTER TABLE relay_jobs
  DROP COLUMN IF EXISTS required_capability,
  DROP COLUMN IF EXISTS consent_state,
  DROP COLUMN IF EXISTS attested_at,
  DROP COLUMN IF EXISTS accepted_at,
  DROP COLUMN IF EXISTS expires_at;

DROP TABLE IF EXISTS notification_preferences;
DROP INDEX IF EXISTS idx_device_one_time_prekeys_available;
DROP TABLE IF EXISTS device_one_time_prekeys;
DROP TABLE IF EXISTS device_identity_keys;
DROP INDEX IF EXISTS idx_user_conversation_state_visibility;

ALTER TABLE user_conversation_state
  DROP COLUMN IF EXISTS muted_until,
  DROP COLUMN IF EXISTS is_pinned,
  DROP COLUMN IF EXISTS is_archived;

ALTER TABLE conversations
  DROP COLUMN IF EXISTS encryption_epoch,
  DROP COLUMN IF EXISTS encryption_state,
  DROP COLUMN IF EXISTS avatar_url,
  DROP COLUMN IF EXISTS created_by_user_id;
