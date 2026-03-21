-- Rollback: Immutable Release Packaging
-- Date: 2026-03-21

DROP TABLE IF EXISTS miniapp_release_asset_references;

DROP INDEX IF EXISTS idx_miniapp_releases_immutable;
DROP INDEX IF EXISTS idx_miniapp_releases_manifest_content_hash;

ALTER TABLE miniapp_registry_releases
  DROP COLUMN IF EXISTS manifest_content_hash,
  DROP COLUMN IF EXISTS asset_set_hash,
  DROP COLUMN IF EXISTS immutable_at;
