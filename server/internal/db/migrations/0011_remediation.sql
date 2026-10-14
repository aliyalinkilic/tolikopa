-- 0011_remediation.sql — endpoint hygiene (bloatware removal / startup control).
--
-- ── AUTHORITY MODEL (read this before touching any table below) ───────────────
-- The agent carries a COMPILED allowlist (agent/internal/appcatalog) and a local
-- GPO kill switch. The server can only ever SELECT FROM that vocabulary: an
-- item_id the agent does not recognise is refused ON THE ENDPOINT and reported
-- back as outcome 'unknown_item'. Extending the vocabulary requires shipping a
-- new tolikopa-agent.exe through the audited GPO path — never a database write.
--
-- These tables are therefore a UI MIRROR and an AUDIT TRAIL, never a command
-- channel (CLAUDE.md §Safety). Nothing here can make an endpoint do something
-- its own binary does not already permit, and both the local kill switch and
-- remediation_policy.dry_run veto independently (the more restrictive wins).
--
-- machine_remediation_state records the presence of ALLOWLISTED items ONLY. It is
-- NOT a general installed-software inventory: software a user installed for
-- themselves never appears in it and never leaves the machine.
--
-- Catalog CONTENT is deliberately NOT seeded here. Bloatware catalogs churn with
-- every Windows feature update, migrations are append-only forever, and
-- ON CONFLICT DO NOTHING cannot express retirement. appcatalog.Sync() mirrors the
-- binary's embedded catalog into remediation_item at startup instead.

-- ── The closed vocabulary, mirrored from the agent's compiled catalog ──────────
CREATE TABLE IF NOT EXISTS remediation_item (
  kind          text NOT NULL,                    -- app | startup
  item_id       text NOT NULL,                    -- catalog key ([a-z0-9][a-z0-9._-]{0,63})
  family        text NOT NULL,                    -- PackageFamilyName, or the Run VALUE NAME
  display_name  text NOT NULL,                    -- Turkish, admin-facing
  category      text,
  risk_tier     text,                             -- safe | review | advanced
  scope         text,                             -- user | provisioned | both (apps)
  safe_default  boolean NOT NULL DEFAULT false,
  process_names text[] NOT NULL DEFAULT '{}',     -- ties an item to existing process telemetry
  notes         text,
  deprecated    boolean NOT NULL DEFAULT false,   -- retired from the vocabulary; never deleted
  sort_order    int NOT NULL DEFAULT 0,           -- preserves catalog.json's curated order
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (kind, item_id)
);
COMMENT ON TABLE remediation_item IS
  'Read-only mirror of the agent''s COMPILED allowlist, refreshed at startup by appcatalog.Sync(). Rows are retired (deprecated=true), never deleted.';

-- ── Fleet-wide policy (singleton, mirrors derive_state from 0005) ──────────────
-- DEFAULTS ARE OFF AND DRY-RUN ON PURPOSE: a fresh install must never actuate.
CREATE TABLE IF NOT EXISTS remediation_policy (
  id            int PRIMARY KEY DEFAULT 1,
  enabled       boolean NOT NULL DEFAULT false,   -- false = the agent reconciles nothing
  dry_run       boolean NOT NULL DEFAULT true,    -- true  = evaluate + report, never actuate
  excluded_tags text[] NOT NULL DEFAULT '{}',     -- machine.tags overlapping these are exempt
  revision      bigint NOT NULL DEFAULT 0,        -- bumped on every admin write (handshake key)
  updated_at    timestamptz NOT NULL DEFAULT now(),
  updated_by    text,
  CONSTRAINT remediation_policy_singleton CHECK (id = 1)
);
INSERT INTO remediation_policy (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- ── The selected subset of the vocabulary ─────────────────────────────────────
-- NO FOREIGN KEY to remediation_item ON PURPOSE: an agent newer than the server
-- (or a catalog rolled back) would otherwise turn every ingest into a 500. The
-- admin write path validates against the embedded catalog and rejects unknown
-- ids with 400; that is where the check belongs.
CREATE TABLE IF NOT EXISTS remediation_policy_item (
  kind     text NOT NULL,
  item_id  text NOT NULL,
  added_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (kind, item_id)
);

-- ── Who armed a fleet-wide destructive capability, and when ───────────────────
CREATE TABLE IF NOT EXISTS remediation_policy_audit (
  id       bigserial PRIMARY KEY,
  ts       timestamptz NOT NULL DEFAULT now(),
  actor    text,                                  -- JWT Subject (panel username)
  actor_ip text,
  revision bigint NOT NULL,                       -- the revision this write produced
  "before" jsonb NOT NULL DEFAULT '{}',
  "after"  jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS remediation_policy_audit_ts_idx ON remediation_policy_audit (ts DESC);
COMMENT ON TABLE remediation_policy_audit IS
  'Permanent record of every policy write. Never truncated by a retention policy.';

-- ── Current per-machine presence (plain current-state table, like machine_live) ─
CREATE TABLE IF NOT EXISTS machine_remediation_state (
  machine_id      text NOT NULL REFERENCES machine(machine_id) ON DELETE CASCADE,
  kind            text NOT NULL,
  item_id         text NOT NULL,
  present         boolean NOT NULL DEFAULT false,
  scope           text,                           -- apps: user | provisioned | both
  enabled         boolean,                        -- startup: runs at logon? NULL = n/a
  first_seen      timestamptz NOT NULL DEFAULT now(),
  last_seen       timestamptz NOT NULL DEFAULT now(),
  last_present_at timestamptz,
  removed_at      timestamptz,                    -- stamped on the present->absent edge
  PRIMARY KEY (machine_id, kind, item_id)
);
-- Partial index: the panel only ever counts/lists what is still present.
CREATE INDEX IF NOT EXISTS machine_remediation_state_present_idx
  ON machine_remediation_state (kind, item_id) WHERE present;
COMMENT ON TABLE machine_remediation_state IS
  'Presence of ALLOWLISTED catalog items only. This is NOT a software inventory: anything outside the agent''s compiled catalog is never inspected, never transmitted and never stored.';

-- ── Audited actions (hypertable) ──────────────────────────────────────────────
-- error_code is a CODE ('0x80073cfa') only — never an OS message or a path, both
-- of which would leak filesystem detail and violate the privacy allowlist.
CREATE TABLE IF NOT EXISTS remediation_attempt (
  ts          timestamptz NOT NULL,
  machine_id  text NOT NULL,
  kind        text NOT NULL,
  item_id     text NOT NULL,
  action      text NOT NULL,   -- remove | deprovision | disable | enable
  outcome     text NOT NULL,   -- ok|failed|dry_run|not_present|not_allowed|blocked|timeout|unknown_item
  error_code  text,
  duration_ms int,
  received_at timestamptz NOT NULL DEFAULT now()
);
SELECT create_hypertable('remediation_attempt', 'ts', chunk_time_interval => INTERVAL '7 days', if_not_exists => TRUE);
-- Every hypertable unique key includes ts -> ON CONFLICT (…, ts) DO NOTHING idempotency.
CREATE UNIQUE INDEX IF NOT EXISTS remediation_attempt_pk
  ON remediation_attempt (machine_id, ts, kind, item_id, action);
CREATE INDEX IF NOT EXISTS remediation_attempt_item_idx    ON remediation_attempt (kind, item_id, ts DESC);
CREATE INDEX IF NOT EXISTS remediation_attempt_outcome_idx ON remediation_attempt (outcome, ts DESC);
-- Retention 365 d, NO COMPRESSION — same reasoning as `event` (0002): the volume
-- is trivial (a handful of rows per machine per policy change) and compressed
-- chunks complicate the late-arriving inserts a spooling agent produces.
SELECT add_retention_policy('remediation_attempt', INTERVAL '365 days', if_not_exists => TRUE);

-- ── Per-machine hygiene posture on the inventory row ──────────────────────────
-- remediation_allowed is the machine's LOCAL veto, collapsed to one boolean:
-- true only when the GPO kill switches let the agent actuate at all
-- (enforce AND (apps OR startup)). false = report-only, whatever the panel says.
ALTER TABLE machine ADD COLUMN IF NOT EXISTS remediation_allowed         boolean;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS remediation_catalog_version text;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS remediation_policy_revision text;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS remediation_reported_at     timestamptz;
COMMENT ON COLUMN machine.remediation_catalog_version IS
  'Vocabulary hash the agent reported (v1-…). Differing from the server''s embedded version means the agent binary is out of date; the panel surfaces it as catalog drift.';
