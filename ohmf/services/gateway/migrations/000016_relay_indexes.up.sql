-- Add indexes to speed relay job polling by status and created_at
CREATE INDEX IF NOT EXISTS idx_relay_jobs_status_created_at ON relay_jobs(status, created_at);
