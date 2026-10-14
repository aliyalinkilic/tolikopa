-- Windows Update posture (read-only): counts + timestamps only, no update
-- titles/KB history. NULL pending_updates = unknown/never reported.
ALTER TABLE machine ADD COLUMN IF NOT EXISTS pending_updates int;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS reboot_pending boolean NOT NULL DEFAULT false;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS updates_last_search timestamptz;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS updates_last_install timestamptz;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS updates_checked_at timestamptz;
