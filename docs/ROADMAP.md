# Roadmap

## v1.0 — Core (this delivery)

- [x] Architecture, schema, API contract, privacy allowlist.
- [ ] Agent: Windows service + Linux systemd; CPU/RAM/disk/net/process, session/user, SMART,
      crash events; gzip batch + spool.
- [ ] Backend: ingest (delta/category/COPY/backpressure), REST + WS, JWT auth, alert engine,
      usage_session derivation.
- [ ] Panel: Dashboard, Machine Detail, Users, Alerts, Settings, Setup; live WS;
      professional dark design + animations.
- [ ] Deployment: Docker Compose, Caddy (self-signed TLS), `setup.sh`, GPO scripts + guide.
- [ ] Live installation on Ubuntu + self-monitoring (Linux agent).

## v1.1 — Hardening

- [ ] Authenticode signing of the agent + GPO Defender exclusion template.
- [ ] Per-machine enrollment token (instead of a shared token).
- [ ] E-mail notifications (SMTP) — for critical alerts (code ready, to be switched on).
- [ ] Role-based access (admin/viewer) read-only pages.
- [ ] Automatic distribution of the CA via domain GPO.

## v1.2 — Depth

- [ ] True idle time (v2 user-session helper) — optional, subject to approval.
- [ ] Heartbeat/uptime SLA reports (Toolkit `heartbeat_agg`).
- [ ] Hardware inventory (CPU model, total RAM, disk model) — static, low frequency.
- [ ] Export (CSV/PDF reports), scheduled summary e-mails.
- [ ] Backup automation (pgBackRest + cron).

## Known limitations / notes

- Network usage is per machine; "user network" = the machine's network while that user is
  signed in (the best attribution possible under the privacy constraint).
- Top-N process CPU% can exceed 100 on multi-core machines and is truncated by the top-N → the
  category breakdown is presented as a **relative share**.
- Category reclassification applies going forward; history reflects "the classification at the
  time".
