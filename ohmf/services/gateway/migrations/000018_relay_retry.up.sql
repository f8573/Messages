-- Migration: ensure retry_count exists on relay_jobs
ALTER TABLE relay_jobs ADD COLUMN IF NOT EXISTS retry_count INT DEFAULT 0;
