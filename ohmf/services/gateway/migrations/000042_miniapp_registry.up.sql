CREATE TABLE IF NOT EXISTS miniapp_releases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id text NOT NULL,
  version text NOT NULL,
  manifest_id uuid NOT NULL REFERENCES miniapp_manifests(id) ON DELETE CASCADE,
  publisher_user_id uuid NOT NULL,
  visibility text NOT NULL DEFAULT 'public',
  source_type text NOT NULL DEFAULT 'external',
  entrypoint_origin text NOT NULL DEFAULT '',
  preview_origin text NOT NULL DEFAULT '',
  manifest_hash text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (app_id, version)
);

CREATE INDEX IF NOT EXISTS idx_miniapp_releases_app_id
  ON miniapp_releases (app_id, published_at DESC);

CREATE INDEX IF NOT EXISTS idx_miniapp_releases_manifest_id
  ON miniapp_releases (manifest_id);

CREATE TABLE IF NOT EXISTS miniapp_installs (
  user_id uuid NOT NULL,
  app_id text NOT NULL,
  installed_version text,
  auto_update boolean NOT NULL DEFAULT true,
  enabled boolean NOT NULL DEFAULT true,
  installed_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, app_id)
);

CREATE INDEX IF NOT EXISTS idx_miniapp_installs_app_id
  ON miniapp_installs (app_id, user_id);

INSERT INTO miniapp_releases (
  app_id,
  version,
  manifest_id,
  publisher_user_id,
  visibility,
  source_type,
  entrypoint_origin,
  preview_origin,
  manifest_hash,
  created_at,
  published_at
)
SELECT
  m.manifest->>'app_id',
  m.manifest->>'version',
  m.id,
  m.owner_user_id,
  COALESCE(NULLIF(m.manifest->'metadata'->>'visibility', ''), 'public'),
  CASE
    WHEN COALESCE(m.manifest->'metadata'->>'registry_hosted', 'false') = 'true' THEN 'registry'
    WHEN (m.manifest->'entrypoint'->>'url') LIKE 'http://localhost:%' OR (m.manifest->'entrypoint'->>'url') LIKE 'http://127.0.0.1:%' THEN 'dev'
    WHEN (m.manifest->'entrypoint'->>'url') LIKE 'https://localhost:%' OR (m.manifest->'entrypoint'->>'url') LIKE 'https://127.0.0.1:%' THEN 'dev'
    ELSE 'external'
  END,
  COALESCE(split_part(split_part(m.manifest->'entrypoint'->>'url', '/', 3), '?', 1), ''),
  COALESCE(split_part(split_part(m.manifest->'message_preview'->>'url', '/', 3), '?', 1), ''),
  md5(m.manifest::text),
  m.created_at,
  m.created_at
FROM miniapp_manifests m
WHERE COALESCE(m.manifest->>'app_id', '') <> ''
  AND COALESCE(m.manifest->>'version', '') <> ''
ON CONFLICT (app_id, version) DO NOTHING;
