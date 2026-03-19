DROP INDEX IF EXISTS idx_notifications_status;
DROP INDEX IF EXISTS idx_attachments_status;

ALTER TABLE notifications
  DROP COLUMN IF EXISTS last_error,
  DROP COLUMN IF EXISTS delivered_at,
  DROP COLUMN IF EXISTS delivered;

ALTER TABLE upload_tokens
  DROP COLUMN IF EXISTS completed_object_size,
  DROP COLUMN IF EXISTS expected_checksum_sha256,
  DROP COLUMN IF EXISTS upload_method,
  DROP COLUMN IF EXISTS object_key;

ALTER TABLE attachments
  DROP COLUMN IF EXISTS available_at,
  DROP COLUMN IF EXISTS upload_completed_at,
  DROP COLUMN IF EXISTS checksum_sha256,
  DROP COLUMN IF EXISTS status,
  DROP COLUMN IF EXISTS file_name;

ALTER TABLE devices
  DROP COLUMN IF EXISTS push_subscription_updated_at,
  DROP COLUMN IF EXISTS push_subscription,
  DROP COLUMN IF EXISTS push_provider;
