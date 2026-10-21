-- 0019_alert_tuning.sql — retune noisy alert defaults; work hours live in app_setting.
--
-- Observed on a production fleet:
--   * mem_high was the noisiest rule; most firings were at 90–91 % against the
--     90 % warn threshold, and many re-opened within 30 minutes of resolving. A
--     Windows desktop at 90 % committed memory is normal, not an incident.
--     New defaults: warn 95 %, crit 98 % (plus a 5-point hysteresis in the engine).
--   * Only rows still at the 0004 seed values are changed: an admin's own
--     thresholds are never overwritten.
UPDATE alert_rule SET warn_threshold = 95, crit_threshold = 98, updated_at = now()
WHERE rule = 'mem_high' AND warn_threshold = 90 AND crit_threshold = 97;

-- crash_storm: document the optional ignore list (process base names whose
-- crashes never count toward a storm) by giving the key an explicit empty value.
UPDATE alert_rule SET params = params || '{"ignore_apps": []}'::jsonb, updated_at = now()
WHERE rule = 'crash_storm' AND NOT (params ? 'ignore_apps');

-- The work-hours schedule (app_setting key 'work_hours') needs no row: until an
-- admin saves one the server uses Mon–Fri 09:00–17:30 Europe/Istanbul, grace
-- 60 min, offline_after 60 min and NO tags (internal/workhours.Default): with an
-- empty tag list no machine is work-hours-only, so offline alerts stay 24/7.
COMMENT ON TABLE app_setting IS
  'Instance-wide settings as jsonb: org_name, default_lang, require_2fa, work_hours, setup state.';
