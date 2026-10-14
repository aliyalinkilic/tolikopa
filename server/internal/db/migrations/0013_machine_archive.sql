-- 0013_machine_archive.sql — retire machines from the panel without losing data.
--
-- Archiving is a VISIBILITY change, not a delete: the machine drops out of every
-- current-state surface (fleet list, overview counts, alerts, live WebSocket,
-- hygiene posture) while its history stays in the hypertables until ordinary
-- retention ages it out. Historical aggregates keyed by user (net_user_1h,
-- proc_cat_user_1h, …) keep counting it — that usage really happened.
--
-- An archived machine whose agent reports again is restored automatically at
-- ingest (action 'auto_restore'): an accidental archive therefore loses nothing,
-- and a PC re-imaged under the same state directory simply comes back.

ALTER TABLE machine ADD COLUMN IF NOT EXISTS archived_at timestamptz;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS archived_by text;
COMMENT ON COLUMN machine.archived_at IS
  'Set = retired from the panel (hidden from current-state views). NULL = active. Cleared automatically when the agent reports again.';

-- Current-state queries filter on "archived_at IS NULL"; the archive list reads the rest.
CREATE INDEX IF NOT EXISTS machine_archived_idx ON machine (archived_at) WHERE archived_at IS NOT NULL;

-- Permanent record of who retired or restored which machine. Never truncated.
CREATE TABLE IF NOT EXISTS machine_archive_audit (
  id         bigserial PRIMARY KEY,
  ts         timestamptz NOT NULL DEFAULT now(),
  machine_id text NOT NULL,
  hostname   text,
  action     text NOT NULL,   -- archive | restore | auto_restore
  actor      text,            -- panel username; NULL for auto_restore
  actor_ip   text
);
CREATE INDEX IF NOT EXISTS machine_archive_audit_ts_idx ON machine_archive_audit (ts DESC);
