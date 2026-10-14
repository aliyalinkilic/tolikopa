-- 0001_init.sql — extensions + regular (non-hypertable) tables.
-- The migration runner executes statements one-by-one in autocommit, so statements
-- that cannot run inside a transaction block (continuous aggregates in 0003) are safe.

CREATE EXTENSION IF NOT EXISTS timescaledb;
-- NOTE: the core design uses only core TimescaleDB (create_hypertable, continuous aggregates,
-- time_bucket, last()). timescaledb_toolkit (state_agg/heartbeat_agg/counter_agg) is NOT required
-- and is intentionally not created here, so the standard Alpine image works. usage_session
-- derivation and net deltas are computed in Go. Toolkit is a documented future upgrade.

-- ── Fleet inventory ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS machine (
  machine_id    text PRIMARY KEY,                 -- stable UUID persisted by the agent
  hostname      text NOT NULL,
  os            text,
  os_build      text,
  agent_version text,
  domain        text,
  kind          text NOT NULL DEFAULT 'windows',  -- windows | linux
  labels        jsonb NOT NULL DEFAULT '{}',      -- admin-set: room / owner / dept
  first_seen    timestamptz NOT NULL DEFAULT now(),
  last_seen     timestamptz
);

-- One row per machine, upserted every batch. Powers the fleet overview + WebSocket
-- WITHOUT touching the hypertables. Keeps the "live" read path O(#machines).
CREATE TABLE IF NOT EXISTS machine_live (
  machine_id        text PRIMARY KEY REFERENCES machine(machine_id) ON DELETE CASCADE,
  ts                timestamptz NOT NULL,
  cpu_pct           real,
  mem_used          bigint,
  mem_total         bigint,
  disk_min_free_pct real,                         -- worst mount's free %
  net_sent_bytes    bigint,                       -- last cumulative counter (for delta calc)
  net_recv_bytes    bigint,
  boot_time         timestamptz,                  -- for counter-reset detection
  logon_user        text,                         -- domain\user of the console session (may be empty)
  session_state     text,                         -- active | idle | locked | disconnected | none
  open_alerts       int NOT NULL DEFAULT 0
);

-- ── Admin panel accounts ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS admin_user (
  id            bigserial PRIMARY KEY,
  username      text UNIQUE NOT NULL,
  password_hash text NOT NULL,                    -- bcrypt, cost >= 12
  role          text NOT NULL DEFAULT 'admin',    -- admin | viewer
  disabled      boolean NOT NULL DEFAULT false,
  created_at    timestamptz NOT NULL DEFAULT now()
);

-- ── Program-type classification (admin-editable) ───────────────────────────────
CREATE TABLE IF NOT EXISTS process_category (
  id          smallserial PRIMARY KEY,
  category    text NOT NULL,                       -- browser|office|dev|media|comms|system|other
  match_type  text NOT NULL DEFAULT 'exact',       -- exact | prefix | glob
  match_value text NOT NULL,                       -- normalized exe basename (lowercase, no .exe)
  priority    int  NOT NULL DEFAULT 100,           -- lower wins
  enabled     boolean NOT NULL DEFAULT true,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (match_type, match_value)
);

-- ── Current disk SMART state (upserted) ────────────────────────────────────────
CREATE TABLE IF NOT EXISTS disk_health (
  machine_id   text NOT NULL REFERENCES machine(machine_id) ON DELETE CASCADE,
  disk         text NOT NULL,
  smart_ok     boolean,
  predict_fail boolean,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (machine_id, disk)
);

-- ── Derived active-usage sessions (materialized by the alerts/derivation job) ───
CREATE TABLE IF NOT EXISTS usage_session (
  id         bigserial PRIMARY KEY,
  machine_id text NOT NULL,
  logon_user text,
  kind       text NOT NULL,                        -- active | logged_on
  started_at timestamptz NOT NULL,
  ended_at   timestamptz,
  is_open    boolean NOT NULL DEFAULT true,
  UNIQUE (machine_id, kind, started_at)
);
CREATE INDEX IF NOT EXISTS usage_session_user_idx    ON usage_session (logon_user, started_at DESC);
CREATE INDEX IF NOT EXISTS usage_session_machine_idx ON usage_session (machine_id, started_at DESC);

-- ── Alert rules (admin-tunable thresholds) ─────────────────────────────────────
CREATE TABLE IF NOT EXISTS alert_rule (
  id             smallserial PRIMARY KEY,
  rule           text UNIQUE NOT NULL,             -- offline|cpu_high|mem_high|disk_low|smart_fail|crash_storm
  enabled        boolean NOT NULL DEFAULT true,
  warn_threshold double precision,
  crit_threshold double precision,
  for_seconds    int NOT NULL DEFAULT 0,           -- sustained window (hysteresis)
  params         jsonb NOT NULL DEFAULT '{}',
  updated_at     timestamptz NOT NULL DEFAULT now()
);

-- ── Alert instances (state machine) ────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS alert (
  id           bigserial PRIMARY KEY,
  machine_id   text,
  rule         text NOT NULL,
  dedup_key    text NOT NULL,                      -- e.g. "cpu_high:<machine>" or "disk_low:<machine>:C:"
  severity     text NOT NULL,                      -- info | warning | critical
  status       text NOT NULL DEFAULT 'open',       -- open | ack | resolved
  value        double precision,
  message      text,
  opened_at    timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  acked_at     timestamptz,
  resolved_at  timestamptz,
  detail       jsonb NOT NULL DEFAULT '{}'
);
-- At most one non-resolved alert per dedup_key.
CREATE UNIQUE INDEX IF NOT EXISTS alert_open_dedup_idx ON alert (dedup_key) WHERE status <> 'resolved';
CREATE INDEX IF NOT EXISTS alert_machine_idx ON alert (machine_id, opened_at DESC);
CREATE INDEX IF NOT EXISTS alert_status_idx  ON alert (status, severity);
