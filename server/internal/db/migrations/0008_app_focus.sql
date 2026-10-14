-- 0008_app_focus.sql — per-application foreground (active-use) time.
-- PRIVACY: app_name is the process BASE NAME only (e.g. "chrome", "excel") —
-- never a window title, document, browser tab, or URL. Powers the per-user
-- "time in apps" view.

CREATE TABLE IF NOT EXISTS app_focus_sample (
  ts          timestamptz NOT NULL,
  machine_id  text NOT NULL,
  logon_user  text,
  app_name    text NOT NULL,
  seconds     int  NOT NULL
);
SELECT create_hypertable('app_focus_sample', 'ts', chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE);
CREATE UNIQUE INDEX IF NOT EXISTS app_focus_sample_pk   ON app_focus_sample (machine_id, ts, app_name);
CREATE INDEX        IF NOT EXISTS app_focus_user_idx    ON app_focus_sample (logon_user, ts);

ALTER TABLE app_focus_sample SET (timescaledb.compress, timescaledb.compress_segmentby = 'machine_id', timescaledb.compress_orderby = 'ts DESC');
SELECT add_compression_policy('app_focus_sample', INTERVAL '7 days', if_not_exists => TRUE);
SELECT add_retention_policy('app_focus_sample', INTERVAL '90 days', if_not_exists => TRUE);

-- Per USER / app / local hour (time-in-apps breakdown per user).
CREATE MATERIALIZED VIEW IF NOT EXISTS app_focus_user_1h
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 hour', ts, 'Europe/Istanbul') AS bucket,
       logon_user,
       app_name,
       sum(seconds) AS seconds
FROM app_focus_sample
WHERE logon_user IS NOT NULL AND logon_user <> ''
GROUP BY 1, 2, 3
WITH NO DATA;

SELECT add_continuous_aggregate_policy('app_focus_user_1h',
  start_offset => INTERVAL '1 day', end_offset => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour', if_not_exists => TRUE);
