-- Mini-app manifests and sessions
CREATE TABLE miniapp_manifests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_user_id uuid NOT NULL,
  manifest jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE miniapp_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  manifest_id uuid REFERENCES miniapp_manifests(id) ON DELETE CASCADE,
  conversation_id uuid,
  participants jsonb,
  state jsonb,
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  ended_at timestamptz
);

CREATE INDEX idx_miniapp_manifests_owner ON miniapp_manifests(owner_user_id);
CREATE INDEX idx_miniapp_sessions_conv ON miniapp_sessions(conversation_id);