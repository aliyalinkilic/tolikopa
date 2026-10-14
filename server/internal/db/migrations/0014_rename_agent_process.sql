-- The Linux agent binary was renamed; classify the new process name as 'system' on
-- databases seeded before the rename (0004 already carries it for fresh installs).
INSERT INTO process_category (category, match_type, match_value, priority) VALUES
  ('system','exact','tolikopa-agent',10)
ON CONFLICT (match_type, match_value) DO NOTHING;
