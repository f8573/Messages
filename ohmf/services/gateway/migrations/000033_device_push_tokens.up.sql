-- Device push tokens for multi-provider push notifications (FCM, APNs)
CREATE TABLE device_push_tokens (
  id BIGSERIAL PRIMARY KEY,
  device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider_type TEXT NOT NULL,  -- 'fcm', 'apns'
  push_token TEXT NOT NULL,
  registered_at TIMESTAMPTZ DEFAULT now(),
  last_verified_at TIMESTAMPTZ,
  UNIQUE(device_id, provider_type)
);

-- Index for efficient token lookups by user and provider
CREATE INDEX idx_device_push_tokens_user_provider
  ON device_push_tokens(user_id, provider_type);

-- Index for efficient token cleanup by last verified time
CREATE INDEX idx_device_push_tokens_last_verified
  ON device_push_tokens(last_verified_at);
