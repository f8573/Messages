-- Migration: Immutable Release Packaging
-- Date: 2026-03-21
--
-- Enforces immutability of approved releases by:
-- 1. Binding each release to a manifest content hash
-- 2. Computing and validating hashes at approval time
-- 3. Tracking asset set hashes for complete integrity
-- 4. Recording immutable timestamps

-- Add immutability tracking columns to releases table
ALTER TABLE miniapp_registry_releases
  ADD COLUMN IF NOT EXISTS manifest_content_hash TEXT,
  ADD COLUMN IF NOT EXISTS asset_set_hash TEXT,
  ADD COLUMN IF NOT EXISTS immutable_at timestamptz;

-- Index for manifest content hash lookups
CREATE INDEX IF NOT EXISTS idx_miniapp_releases_manifest_content_hash
  ON miniapp_registry_releases (manifest_content_hash)
  WHERE manifest_content_hash IS NOT NULL;

-- Index for approved immutable releases
CREATE INDEX IF NOT EXISTS idx_miniapp_releases_immutable
  ON miniapp_registry_releases (app_id, immutable_at DESC)
  WHERE immutable_at IS NOT NULL;

-- Table for tracking individual asset hashes per release
-- Supports future migration to content-addressed storage
CREATE TABLE IF NOT EXISTS miniapp_release_asset_references (
  app_id text NOT NULL,
  version text NOT NULL,
  asset_path text NOT NULL,
  asset_hash text NOT NULL,
  asset_type text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (app_id, version, asset_path),
  FOREIGN KEY (app_id, version) REFERENCES miniapp_registry_releases(app_id, version) ON DELETE CASCADE
);

-- Index for querying assets by release
CREATE INDEX IF NOT EXISTS idx_miniapp_release_asset_references_release
  ON miniapp_release_asset_references (app_id, version, created_at DESC);

-- Index for content-based lookups (future: content-addressed storage)
CREATE INDEX IF NOT EXISTS idx_miniapp_release_asset_references_hash
  ON miniapp_release_asset_references (asset_hash);
