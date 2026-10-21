-- 0020_remediation_switches.sql — keep the three local kill switches apart.
--
-- 0011 stored the agent's allowed{apps,startup,enforce} collapsed into one
-- boolean (remediation_allowed). That answers "can this machine act at all?" but
-- not "can it disable startup entries?" — and the Optimization page has to tell
-- an admin which switch to turn on, per machine. The agent has always sent all
-- three on the wire; nothing new is collected, the server simply stops throwing
-- two bits away. NULL = the machine has not reported since this migration.
ALTER TABLE machine ADD COLUMN IF NOT EXISTS remediation_allow_apps    boolean;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS remediation_allow_startup boolean;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS remediation_enforce       boolean;
COMMENT ON COLUMN machine.remediation_allow_apps IS
  'Local GPO switch allow_app_removal as last reported by the agent; NULL = not reported since 0020.';
COMMENT ON COLUMN machine.remediation_allow_startup IS
  'Local GPO switch allow_startup_control as last reported by the agent; NULL = not reported since 0020.';
COMMENT ON COLUMN machine.remediation_enforce IS
  'Local GPO switch enforce_actions as last reported by the agent; NULL = not reported since 0020.';
