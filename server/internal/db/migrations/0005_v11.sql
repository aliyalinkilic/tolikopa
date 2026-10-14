-- 0005_v11.sql — v1.1 additions.
-- Idempotent (IF NOT EXISTS) so re-runs are safe.

-- ── Hardware specs on the machine inventory ────────────────────────────────────
ALTER TABLE machine ADD COLUMN IF NOT EXISTS cpu_model text;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS cpu_cores int;

-- ── User profiles: display name + department tags (admin-editable) ──────────────
CREATE TABLE IF NOT EXISTS user_profile (
  logon_user   text PRIMARY KEY,
  display_name text,
  tags         text[] NOT NULL DEFAULT '{}',
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS user_profile_tags_idx ON user_profile USING gin (tags);

-- ── Persisted cursor for the alerts-engine session derivation ──────────────────
-- Survives server restarts so usage_session isn't lost or re-scanned. Singleton row.
CREATE TABLE IF NOT EXISTS derive_state (
  id             int PRIMARY KEY DEFAULT 1,
  last_processed timestamptz NOT NULL DEFAULT now() - INTERVAL '15 minutes',
  CONSTRAINT derive_state_singleton CHECK (id = 1)
);
INSERT INTO derive_state (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
