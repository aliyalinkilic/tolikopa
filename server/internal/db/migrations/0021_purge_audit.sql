-- 0021_purge_audit.sql — a purge keeps the machine's hygiene audit trail.
--
-- POST /api/admin/machines/purge no longer deletes remediation_attempt rows:
-- they are the actuation audit trail ("what did the agent remove on PC-042?")
-- and age out with the table's 365-day retention like every other attempt. The
-- 'purge' audit row records how many of them were kept, and the admin attempt
-- feed reads the deleted machine's hostname from it.
ALTER TABLE machine_archive_audit ADD COLUMN IF NOT EXISTS retained_attempts integer;
COMMENT ON COLUMN machine_archive_audit.retained_attempts IS
  'purge only: remediation_attempt rows of the machine kept as audit trail at purge time; NULL for other actions.';
COMMENT ON COLUMN machine_archive_audit.action IS
  'archive | restore | auto_restore | purge';
CREATE INDEX IF NOT EXISTS machine_archive_audit_machine_idx
  ON machine_archive_audit (machine_id, ts DESC);
