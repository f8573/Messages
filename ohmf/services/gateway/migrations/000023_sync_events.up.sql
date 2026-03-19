ALTER TABLE conversation_members
  ADD COLUMN IF NOT EXISTS last_delivered_server_order BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS domain_events (
  event_id BIGSERIAL PRIMARY KEY,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_domain_events_pending
  ON domain_events (processed_at, event_id);

CREATE TABLE IF NOT EXISTS user_inbox_events (
  user_event_id BIGSERIAL PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id UUID REFERENCES conversations(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_inbox_events_user_cursor
  ON user_inbox_events (user_id, user_event_id);

CREATE TABLE IF NOT EXISTS user_conversation_state (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  last_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
  last_message_preview TEXT,
  last_message_at TIMESTAMPTZ,
  unread_count BIGINT NOT NULL DEFAULT 0,
  last_read_server_order BIGINT NOT NULL DEFAULT 0,
  last_delivered_server_order BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, conversation_id)
);

CREATE INDEX IF NOT EXISTS idx_user_conversation_state_user_updated
  ON user_conversation_state (user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS user_device_cursors (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL,
  last_user_event_id BIGINT NOT NULL DEFAULT 0,
  last_delivered_server_order BIGINT NOT NULL DEFAULT 0,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, device_id)
);

INSERT INTO user_conversation_state (
  user_id,
  conversation_id,
  last_message_id,
  last_message_preview,
  last_message_at,
  unread_count,
  last_read_server_order,
  last_delivered_server_order,
  updated_at
)
SELECT
  cm.user_id,
  c.id,
  c.last_message_id,
  '',
  c.updated_at,
  0,
  cm.last_read_server_order,
  cm.last_delivered_server_order,
  c.updated_at
FROM conversation_members cm
JOIN conversations c ON c.id = cm.conversation_id
ON CONFLICT (user_id, conversation_id) DO NOTHING;
