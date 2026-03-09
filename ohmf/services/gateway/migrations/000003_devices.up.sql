ALTER TABLE devices
  ADD COLUMN IF NOT EXISTS client_version TEXT,
  ADD COLUMN IF NOT EXISTS capabilities TEXT[],
  ADD COLUMN IF NOT EXISTS sms_role_state TEXT;

-- create a GIN index for capabilities array lookups
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relname = 'idx_devices_capabilities' AND n.nspname = 'public'
  ) THEN
    CREATE INDEX idx_devices_capabilities ON devices USING GIN (capabilities);
  END IF;
END $$;
