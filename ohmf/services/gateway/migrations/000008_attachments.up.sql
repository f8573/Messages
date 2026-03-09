CREATE TABLE attachments (
  attachment_id UUID PRIMARY KEY,
  message_id UUID REFERENCES messages(id),
  object_key TEXT,
  thumbnail_key TEXT,
  mime_type TEXT NOT NULL,
  size_bytes BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  deleted_at TIMESTAMP,
  redacted_at TIMESTAMP
);

CREATE INDEX idx_attachments_message ON attachments(message_id);
