DROP INDEX IF EXISTS idx_account_deletion_audit_status_effective;
DROP INDEX IF EXISTS idx_account_deletion_audit_user_requested;
DROP TABLE IF EXISTS account_deletion_audit;

ALTER TABLE users
  DROP COLUMN IF EXISTS deletion_reason,
  DROP COLUMN IF EXISTS deletion_completed_at,
  DROP COLUMN IF EXISTS deletion_effective_at,
  DROP COLUMN IF EXISTS deletion_requested_at,
  DROP COLUMN IF EXISTS deletion_state;
