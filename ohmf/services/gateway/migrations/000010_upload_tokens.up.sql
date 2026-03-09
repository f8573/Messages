-- Upload tokens for presigned uploads
CREATE TABLE upload_tokens (
  token uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  attachment_id uuid REFERENCES attachments(attachment_id) ON DELETE CASCADE,
  mime_type text NOT NULL,
  size_bytes bigint,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);

CREATE INDEX idx_upload_tokens_attachment ON upload_tokens(attachment_id);