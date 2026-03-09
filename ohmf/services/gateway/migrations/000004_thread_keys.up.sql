CREATE TABLE IF NOT EXISTS conversation_thread_keys (
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  value TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (conversation_id, kind, value)
);

CREATE INDEX IF NOT EXISTS idx_thread_keys_kind ON conversation_thread_keys(kind);
