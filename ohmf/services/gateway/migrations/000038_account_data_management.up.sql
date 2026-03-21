ALTER TABLE users
  ADD COLUMN IF NOT EXISTS deletion_state TEXT NOT NULL DEFAULT 'ACTIVE',
  ADD COLUMN IF NOT EXISTS deletion_requested_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deletion_effective_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deletion_completed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deletion_reason TEXT;

CREATE TABLE IF NOT EXISTS account_deletion_audit (
  id BIGSERIAL PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  effective_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'PENDING',
  reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_account_deletion_audit_user_requested
  ON account_deletion_audit(user_id, requested_at DESC);

CREATE INDEX IF NOT EXISTS idx_account_deletion_audit_status_effective
  ON account_deletion_audit(status, effective_at);
