-- Machine LAN address. Two witnesses, because they disagree in exactly the
-- cases an admin cares about:
--   ip_addrs  — what the agent sees on its own interfaces (authoritative; a
--               machine behind VPN or NAT still reports its real LAN address).
--   last_ip   — the source address the ingest endpoint observed (works for
--               agents older than this column; wrong for the Linux agent on the
--               server itself, which arrives via the Docker bridge).
ALTER TABLE machine ADD COLUMN IF NOT EXISTS ip_addrs      text[] NOT NULL DEFAULT '{}';
ALTER TABLE machine ADD COLUMN IF NOT EXISTS last_ip       inet;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS ip_updated_at timestamptz;
COMMENT ON COLUMN machine.ip_addrs IS
  'Agent-reported own IPv4 addresses (LAN identity, like hostname). Never peers or per-process connections.';
COMMENT ON COLUMN machine.last_ip IS
  'Source address of the last ingest request as seen behind Caddy (X-Forwarded-For).';
