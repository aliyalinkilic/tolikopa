-- 0010_usage_stats.sql — usage/app-consumption rollups + real-time aggregation
-- for the homepage dashboard, and the new "idle" session state.
--
-- NOTE: creating a continuous aggregate cannot run inside a transaction block;
-- the migration runner executes each statement in autocommit, so this is fine.
--
-- session_state now includes "idle" (logged on + unlocked, no user input >= 5 min).
-- Full set: active | idle | locked | disconnected | none. No DDL change needed
-- (text column); update the column comments so \d+ stays truthful.
COMMENT ON COLUMN machine_live.session_state   IS 'active | idle | locked | disconnected | none';
COMMENT ON COLUMN metric_sample.session_state  IS 'active | idle | locked | disconnected | none';

-- ── Per machine / user / local hour: session-state sample counts ───────────────
-- Powers the fleet busy-hours heatmap and per-user presence stats.
-- Buckets in Europe/Istanbul (variable-width) -> built directly on the raw
-- hypertable, never as a hierarchical child (see 0003 note).
--
-- DESIGN: rows with an empty/NULL logon_user are INCLUDED, grouped under '' via
-- COALESCE, so a single cagg serves both the per-user dimension (logon_user <> '')
-- and machine-level fleet activity (all rows). Filter logon_user <> '' in queries
-- that need only real users.
--
-- SECONDS CONVENTION: the agent's nominal vitals cadence is 15 s (vitals_sec=15),
-- so wall-clock seconds ~= samples * 15. Documented in docs/DATABASE.md; every
-- API query applies the same multiplier.
CREATE MATERIALIZED VIEW IF NOT EXISTS session_user_1h
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 hour', ts, 'Europe/Istanbul') AS bucket,
       machine_id,
       COALESCE(logon_user, '') AS logon_user,
       count(*) FILTER (WHERE session_state = 'active') AS active_samples,
       count(*) FILTER (WHERE session_state = 'idle')   AS idle_samples,
       count(*) FILTER (WHERE session_state = 'locked') AS locked_samples,
       count(*)                                         AS total_samples
FROM metric_sample
GROUP BY 1, 2, 3
WITH NO DATA;

-- ── Per process NAME / hour (UTC): fleet-wide app consumption ──────────────────
-- Powers "which apps consume the most CPU/RAM and for how long". Process rows
-- ride only on the periodic FULL sample (sample_sec=60), so wall-clock duration
-- ~= samples * 60 s; active_hours = number of distinct hour buckets the app
-- appeared in. UTC fixed-width buckets (not user-facing day boundaries).
-- Names are already normalized at ingest (lowercase basename, no ".exe").
CREATE MATERIALIZED VIEW IF NOT EXISTS proc_name_1h
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 hour', ts) AS bucket,
       process_name,
       sum(cpu_pct)               AS cpu_sum,
       avg(cpu_pct)               AS cpu_avg,
       max(cpu_pct)               AS cpu_max,
       avg(mem_bytes)             AS mem_avg,
       max(mem_bytes)             AS mem_max,
       count(*)                   AS samples,
       count(DISTINCT machine_id) AS machines
FROM process_sample
GROUP BY 1, 2
WITH NO DATA;

-- ── Refresh policies (match the 0003 conventions) ──────────────────────────────
SELECT add_continuous_aggregate_policy('session_user_1h',
  start_offset => INTERVAL '1 day', end_offset => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour', if_not_exists => TRUE);

SELECT add_continuous_aggregate_policy('proc_name_1h',
  start_offset => INTERVAL '1 day', end_offset => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour', if_not_exists => TRUE);

-- ── Real-time aggregation ──────────────────────────────────────────────────────
-- Since TimescaleDB 2.13 caggs default to materialized_only=true; opt these three
-- into real-time aggregation so dashboard queries see the un-materialized head
-- (data newer than the watermark is computed on the fly and unioned in). The
-- metric_5m refresh policy (end_offset 5 min / schedule 5 min, see 0003) stays
-- as-is; real-time aggregation covers the freshness gap.
ALTER MATERIALIZED VIEW metric_5m       SET (timescaledb.materialized_only = false);
ALTER MATERIALIZED VIEW session_user_1h SET (timescaledb.materialized_only = false);
ALTER MATERIALIZED VIEW proc_name_1h    SET (timescaledb.materialized_only = false);

-- No retention policies on caggs: the existing migrations (0003/0008) keep cagg
-- data indefinitely (raw hypertable retention still applies underneath), and the
-- hourly rollups are tiny. Deliberately unchanged here.
