CREATE TABLE IF NOT EXISTS miniapp_registry_apps (
  app_id text PRIMARY KEY,
  name text NOT NULL,
  owner_user_id text NOT NULL,
  visibility text NOT NULL,
  summary text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  latest_version text NOT NULL DEFAULT '',
  latest_approved_version text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS miniapp_registry_releases (
  app_id text NOT NULL REFERENCES miniapp_registry_apps(app_id) ON DELETE CASCADE,
  version text NOT NULL,
  manifest_json jsonb NOT NULL,
  manifest_hash text NOT NULL,
  review_status text NOT NULL,
  review_note text NOT NULL DEFAULT '',
  source_type text NOT NULL,
  visibility text NOT NULL,
  publisher_user_id text NOT NULL,
  supported_platforms jsonb NOT NULL DEFAULT '[]'::jsonb,
  entrypoint_origin text NOT NULL DEFAULT '',
  preview_origin text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  submitted_at timestamptz,
  reviewed_at timestamptz,
  published_at timestamptz,
  revoked_at timestamptz,
  PRIMARY KEY (app_id, version)
);

CREATE INDEX IF NOT EXISTS idx_miniapp_registry_releases_status
  ON miniapp_registry_releases (review_status, app_id, version DESC);

CREATE INDEX IF NOT EXISTS idx_miniapp_registry_releases_publisher
  ON miniapp_registry_releases (publisher_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS miniapp_registry_installs (
  user_id text NOT NULL,
  app_id text NOT NULL REFERENCES miniapp_registry_apps(app_id) ON DELETE CASCADE,
  installed_version text NOT NULL,
  auto_update boolean NOT NULL DEFAULT true,
  enabled boolean NOT NULL DEFAULT true,
  installed_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (user_id, app_id)
);

CREATE INDEX IF NOT EXISTS idx_miniapp_registry_installs_app
  ON miniapp_registry_installs (app_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS miniapp_registry_publisher_keys (
  publisher_user_id text NOT NULL,
  key_id text NOT NULL,
  algorithm text NOT NULL,
  public_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  PRIMARY KEY (publisher_user_id, key_id)
);

CREATE TABLE IF NOT EXISTS miniapp_registry_review_audit_log (
  id bigserial PRIMARY KEY,
  app_id text NOT NULL,
  version text,
  actor_user_id text NOT NULL,
  action text NOT NULL,
  note text NOT NULL DEFAULT '',
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_miniapp_registry_review_audit_app
  ON miniapp_registry_review_audit_log (app_id, created_at DESC);
