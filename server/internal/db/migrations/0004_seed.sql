-- 0004_seed.sql — seed classification map + default alert rules.
-- Idempotent (ON CONFLICT DO NOTHING) so re-runs never clobber admin edits.

-- ── Program-type classification seed ───────────────────────────────────────────
-- match_value = normalized exe basename (lowercase, no ".exe"). exact wins over prefix.
INSERT INTO process_category (category, match_type, match_value, priority) VALUES
  -- browser
  ('browser','exact','chrome',10), ('browser','exact','msedge',10), ('browser','exact','firefox',10),
  ('browser','exact','brave',10), ('browser','exact','opera',10), ('browser','exact','vivaldi',10),
  ('browser','exact','iexplore',10), ('browser','exact','chrome_proxy',10),
  -- office
  ('office','exact','winword',10), ('office','exact','excel',10), ('office','exact','powerpnt',10),
  ('office','exact','outlook',10), ('office','exact','onenote',10), ('office','exact','msaccess',10),
  ('office','exact','mspub',10), ('office','exact','visio',10), ('office','exact','winproj',10),
  ('office','exact','soffice',10), ('office','exact','soffice.bin',10), ('office','exact','wps',10),
  ('office','exact','et',10), ('office','exact','wpp',10), ('office','exact','acrord32',10),
  ('office','exact','acrobat',10),
  -- dev
  ('dev','exact','code',10), ('dev','exact','devenv',10), ('dev','exact','sublime_text',10),
  ('dev','exact','windowsterminal',10), ('dev','exact','wt',10), ('dev','exact','powershell',10),
  ('dev','exact','pwsh',10), ('dev','exact','cmd',10), ('dev','exact','conemu',10),
  ('dev','exact','git',10), ('dev','exact','git-bash',10), ('dev','exact','docker',10),
  ('dev','exact','postman',10), ('dev','exact','dbeaver',10), ('dev','exact','ssms',10),
  ('dev','prefix','idea',50), ('dev','prefix','pycharm',50), ('dev','prefix','goland',50),
  ('dev','prefix','webstorm',50), ('dev','prefix','clion',50), ('dev','prefix','rider',50),
  ('dev','prefix','python',50), ('dev','prefix','java',50), ('dev','prefix','node',50),
  -- media
  ('media','exact','vlc',10), ('media','exact','spotify',10), ('media','exact','wmplayer',10),
  ('media','exact','groove',10), ('media','exact','potplayer',10), ('media','exact','potplayermini64',10),
  ('media','exact','foobar2000',10), ('media','exact','itunes',10), ('media','prefix','mpc-hc',50),
  -- comms
  ('comms','exact','teams',10), ('comms','exact','ms-teams',10), ('comms','exact','slack',10),
  ('comms','exact','zoom',10), ('comms','exact','discord',10), ('comms','exact','skype',10),
  ('comms','exact','whatsapp',10), ('comms','exact','telegram',10), ('comms','exact','thunderbird',10),
  -- system / OS noise (kept out of user app breakdowns)
  ('system','exact','explorer',10), ('system','exact','dwm',10), ('system','exact','svchost',10),
  ('system','exact','services',10), ('system','exact','lsass',10), ('system','exact','csrss',10),
  ('system','exact','wininit',10), ('system','exact','winlogon',10), ('system','exact','smss',10),
  ('system','exact','system',10), ('system','exact','taskhostw',10), ('system','exact','spoolsv',10),
  ('system','exact','searchindexer',10), ('system','exact','sihost',10), ('system','exact','runtimebroker',10),
  ('system','exact','fontdrvhost',10), ('system','exact','ctfmon',10), ('system','exact','conhost',10),
  ('system','exact','dllhost',10), ('system','exact','msmpeng',10), ('system','exact','audiodg',10),
  ('system','exact','securityhealthservice',10), ('system','exact','wudfhost',10),
  ('system','exact','tolikopa-agent',10), ('system','exact','systemd',10), ('system','exact','kthreadd',10)
ON CONFLICT (match_type, match_value) DO NOTHING;

-- ── Default alert rules ────────────────────────────────────────────────────────
-- Thresholds: offline in seconds-since-last_seen; cpu/mem in percent; disk_low in
-- free-percent (lower is worse); smart_fail boolean; crash_storm count-in-window.
INSERT INTO alert_rule (rule, enabled, warn_threshold, crit_threshold, for_seconds, params) VALUES
  ('offline',     true,  300,  1800, 0,   '{}'),
  ('cpu_high',    true,  85,   95,   600, '{}'),
  ('mem_high',    true,  90,   97,   600, '{}'),
  ('disk_low',    true,  15,   5,    0,   '{}'),
  ('smart_fail',  true,  NULL, 1,    0,   '{}'),
  ('crash_storm', true,  3,    6,    0,   '{"window_seconds": 600}')
ON CONFLICT (rule) DO NOTHING;
