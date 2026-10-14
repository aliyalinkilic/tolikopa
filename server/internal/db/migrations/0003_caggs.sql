-- 0003_caggs.sql — continuous aggregates (rollups) + refresh policies.
-- NOTE: creating a continuous aggregate cannot run inside a transaction block;
-- the migration runner executes each statement in autocommit, so this is fine.
-- Hierarchical rollups (metric_5m -> metric_1h) use fixed-width UTC buckets.
-- User-facing rollups bucket in Europe/Istanbul (variable-width) and are built
-- directly on the raw hypertable — never as a hierarchical child.

-- Per machine / 5 min (fixed-width UTC; hierarchical parent).
CREATE MATERIALIZED VIEW IF NOT EXISTS metric_5m
WITH (timescaledb.continuous) AS
SELECT time_bucket('5 minutes', ts) AS bucket,
       machine_id,
       avg(cpu_pct)                                        AS cpu_avg,
       max(cpu_pct)                                        AS cpu_max,
       avg(mem_used::float8 / NULLIF(mem_total, 0))        AS mem_ratio_avg,
       max(mem_used::float8 / NULLIF(mem_total, 0))        AS mem_ratio_max,
       sum(net_sent_delta)                                 AS sent_bytes,
       sum(net_recv_delta)                                 AS recv_bytes,
       last(logon_user, ts)                                AS last_user,
       last(session_state, ts)                             AS last_state,
       count(*)                                            AS samples
FROM metric_sample
GROUP BY 1, 2
WITH NO DATA;

-- Per machine / 1 h (hierarchical on metric_5m; cheap long-range charts).
CREATE MATERIALIZED VIEW IF NOT EXISTS metric_1h
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 hour', bucket) AS bucket,
       machine_id,
       avg(cpu_avg)       AS cpu_avg,
       max(cpu_max)       AS cpu_max,
       avg(mem_ratio_avg) AS mem_ratio_avg,
       max(mem_ratio_max) AS mem_ratio_max,
       sum(sent_bytes)    AS sent_bytes,
       sum(recv_bytes)    AS recv_bytes
FROM metric_5m
GROUP BY 1, 2
WITH NO DATA;

-- Per USER / local hour: network up/down (per-user network timeline).
CREATE MATERIALIZED VIEW IF NOT EXISTS net_user_1h
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 hour', ts, 'Europe/Istanbul') AS bucket,
       logon_user,
       sum(net_sent_delta)          AS sent_bytes,
       sum(net_recv_delta)          AS recv_bytes,
       count(DISTINCT machine_id)   AS machines
FROM metric_sample
WHERE logon_user IS NOT NULL AND logon_user <> ''
GROUP BY 1, 2
WITH NO DATA;

-- Per machine / category / hour (machine-detail category breakdown; UTC).
CREATE MATERIALIZED VIEW IF NOT EXISTS proc_cat_1h
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 hour', ts) AS bucket,
       machine_id,
       category_id,
       avg(cpu_pct)  AS cpu_avg,
       sum(cpu_pct)  AS cpu_sum,
       avg(mem_bytes) AS mem_avg,
       count(*)      AS samples
FROM process_sample
GROUP BY 1, 2, 3
WITH NO DATA;

-- Per USER / category / local hour (program-type breakdown per user).
CREATE MATERIALIZED VIEW IF NOT EXISTS proc_cat_user_1h
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 hour', ts, 'Europe/Istanbul') AS bucket,
       logon_user,
       category_id,
       sum(cpu_pct) AS cpu_sum,
       count(*)     AS samples
FROM process_sample
WHERE logon_user IS NOT NULL AND logon_user <> ''
GROUP BY 1, 2, 3
WITH NO DATA;

-- ── Refresh policies (keep the live edge fresh; each start_offset ≪ raw retention) ──
SELECT add_continuous_aggregate_policy('metric_5m',
  start_offset => INTERVAL '2 hours', end_offset => INTERVAL '5 minutes',
  schedule_interval => INTERVAL '5 minutes', if_not_exists => TRUE);

SELECT add_continuous_aggregate_policy('metric_1h',
  start_offset => INTERVAL '1 day', end_offset => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour', if_not_exists => TRUE);

SELECT add_continuous_aggregate_policy('net_user_1h',
  start_offset => INTERVAL '1 day', end_offset => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour', if_not_exists => TRUE);

SELECT add_continuous_aggregate_policy('proc_cat_1h',
  start_offset => INTERVAL '1 day', end_offset => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour', if_not_exists => TRUE);

SELECT add_continuous_aggregate_policy('proc_cat_user_1h',
  start_offset => INTERVAL '1 day', end_offset => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour', if_not_exists => TRUE);
