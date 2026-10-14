-- 0002_hypertables.sql — time-series hypertables + compression + retention.
-- Every UNIQUE/PK on a hypertable MUST include the partition column `ts`
-- (TimescaleDB constraint) — this also gives us ON CONFLICT (…, ts) DO NOTHING idempotency.

-- ── Per-machine gauges (~1 row / machine / 30s) ────────────────────────────────
CREATE TABLE IF NOT EXISTS metric_sample (
  ts             timestamptz NOT NULL,
  machine_id     text NOT NULL,
  cpu_pct        real,
  mem_used       bigint,
  mem_total      bigint,
  net_sent_bytes bigint,          -- cumulative NIC counter (kept for audit / recompute)
  net_recv_bytes bigint,
  net_sent_delta bigint,          -- per-interval, computed at ingest, reset-corrected
  net_recv_delta bigint,
  boot_time      timestamptz,
  logon_user     text,
  session_state  text,
  received_at    timestamptz NOT NULL DEFAULT now()
);
SELECT create_hypertable('metric_sample', 'ts', chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE);
CREATE UNIQUE INDEX IF NOT EXISTS metric_sample_pk ON metric_sample (machine_id, ts);

-- ── Per-mount disk usage ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS disk_usage_sample (
  ts          timestamptz NOT NULL,
  machine_id  text NOT NULL,
  mount       text NOT NULL,
  used_bytes  bigint,
  total_bytes bigint
);
SELECT create_hypertable('disk_usage_sample', 'ts', chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE);
CREATE UNIQUE INDEX IF NOT EXISTS disk_usage_sample_pk ON disk_usage_sample (machine_id, mount, ts);

-- ── Top-N processes (highest-volume table). logon_user + category_id denormalized
--    at ingest so per-user / per-category rollups are pure GROUP BYs. ──────────────
CREATE TABLE IF NOT EXISTS process_sample (
  ts           timestamptz NOT NULL,
  machine_id   text NOT NULL,
  process_name text NOT NULL,     -- normalized basename, lowercase, no ".exe"
  category_id  smallint,          -- resolved at ingest; NULL = unknown
  logon_user   text,
  cpu_pct      real,
  mem_bytes    bigint
);
SELECT create_hypertable('process_sample', 'ts', chunk_time_interval => INTERVAL '12 hours', if_not_exists => TRUE);
CREATE UNIQUE INDEX IF NOT EXISTS process_sample_pk ON process_sample (machine_id, ts, process_name);

-- ── Discrete events (crashes, hardware/SMART warnings) ─────────────────────────
CREATE TABLE IF NOT EXISTS event (
  ts           timestamptz NOT NULL,
  machine_id   text NOT NULL,
  kind         text NOT NULL,     -- app_crash | app_hang | hw_warning | smart_fail
  process_name text,
  severity     text,
  detail       jsonb NOT NULL DEFAULT '{}'
);
SELECT create_hypertable('event', 'ts', chunk_time_interval => INTERVAL '7 days', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS event_machine_idx ON event (machine_id, ts DESC);
CREATE INDEX IF NOT EXISTS event_kind_idx    ON event (kind, ts DESC);

-- ── Compression (columnstore) — segment ONLY by machine_id (low cardinality ≤50).
--    NEVER segment by process_name; it would explode segment count. ───────────────
ALTER TABLE metric_sample     SET (timescaledb.compress, timescaledb.compress_segmentby = 'machine_id', timescaledb.compress_orderby = 'ts DESC');
ALTER TABLE disk_usage_sample SET (timescaledb.compress, timescaledb.compress_segmentby = 'machine_id', timescaledb.compress_orderby = 'ts DESC');
ALTER TABLE process_sample    SET (timescaledb.compress, timescaledb.compress_segmentby = 'machine_id', timescaledb.compress_orderby = 'ts DESC');

SELECT add_compression_policy('metric_sample',     INTERVAL '7 days',  if_not_exists => TRUE);
SELECT add_compression_policy('disk_usage_sample', INTERVAL '7 days',  if_not_exists => TRUE);
SELECT add_compression_policy('process_sample',    INTERVAL '3 days',  if_not_exists => TRUE);

-- ── Retention ──────────────────────────────────────────────────────────────────
SELECT add_retention_policy('metric_sample',     INTERVAL '90 days', if_not_exists => TRUE);
SELECT add_retention_policy('disk_usage_sample', INTERVAL '90 days', if_not_exists => TRUE);
SELECT add_retention_policy('process_sample',    INTERVAL '21 days', if_not_exists => TRUE);
SELECT add_retention_policy('event',             INTERVAL '365 days', if_not_exists => TRUE);
