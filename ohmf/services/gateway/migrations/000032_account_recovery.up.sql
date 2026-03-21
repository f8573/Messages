-- Table for account recovery codes
CREATE TABLE account_recovery_codes (
  id BIGSERIAL PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code TEXT NOT NULL UNIQUE,
  used BOOLEAN DEFAULT FALSE,
  used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now(),
  expires_at TIMESTAMPTZ
);

-- Table for 2FA methods per user
CREATE TABLE two_factor_methods (
  id BIGSERIAL PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  method_type TEXT,  -- 'sms', 'totp'
  identifier TEXT,   -- Phone number or TOTP secret
  enabled BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Indices for efficient lookups
CREATE INDEX idx_recovery_codes_user
  ON account_recovery_codes(user_id, used);

CREATE INDEX idx_recovery_codes_expires
  ON account_recovery_codes(expires_at);

CREATE INDEX idx_2fa_user_enabled
  ON two_factor_methods(user_id, enabled);
