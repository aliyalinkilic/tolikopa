# Tolikopa — IT Fleet Monitoring

A lightweight, privacy-respecting system that centrally monitors the **hardware health and general
usage** of domain-joined computers. It consists of a small agent pushed silently to every PC via
Group Policy, a collection/database layer running on an Ubuntu server on the same network, and a
modern, animated web admin panel.

> **Privacy:** Screen contents, keystrokes, window titles, URLs and file names are **never
> collected**. Only aggregate system metrics (CPU/RAM/disk/network), process name + category, the
> logged-on username and hardware health are collected. Details: [`docs/AGENT.md`](docs/AGENT.md).

## What does it monitor?

- Live CPU / RAM / disk usage and disk fill levels
- The applications consuming the most resources (name + category + CPU/RAM only)
- Failing hardware (disk SMART) and alerts for crashing/hanging programs
- When PCs are actively in use (session state)
- Classification by program type (browser / office / development / media / ...)
- Network (upload/download) usage per username — on a timeline

## Architecture

```
Windows PCs (Tolikopa agent, Windows service)
        │  HTTPS batch  ─────────────►  Ubuntu server
                                        ├─ Caddy (TLS + SPA + reverse proxy)
                                        ├─ API   (Go + Echo)
                                        └─ TimescaleDB (PostgreSQL 17)
Admin browser  ◄── React panel (live WebSocket)
```

## Repository layout

- [`agent/`](agent/) — Go agent (Windows + Linux). A single static binary, no installer UI.
- [`server/`](server/) — Go backend: ingest, REST/WebSocket API, alert engine.
- [`web/`](web/) — Vite + React + TypeScript admin panel.
- [`setup/`](setup/) — `tolikopa-setup.exe` / `tolikopa-update.exe`: domain rollout tool (Go).
- [`deploy/`](deploy/) — Docker Compose, Caddy, GPO scripts.
- [`setup.sh`](setup.sh) — Ubuntu server installation and updates.
- [`docs/`](docs/) — technical documentation (see below).
- [`CLAUDE.md`](CLAUDE.md) — development guide and conventions.

## Quick start

**1. Server (Ubuntu):**
```bash
git clone https://github.com/aliyalinkilic/tolikopa.git ~/tolikopa && cd ~/tolikopa
sudo ./setup.sh
```
It first checks the prerequisites (Docker, Compose, free ports, disk space, outbound internet
access); it installs nothing itself — if something is missing, it prints the commands to fix it and
stops. It then asks for the panel's DNS name (e.g. `tolikopa.corp.local`), the server's IP address
and the first administrator account; generates the secret keys and a self-signed TLS certificate,
builds and starts the whole stack, and creates the administrator. Finally it prints the panel
address and the certificate fingerprint.

**2. Panel — setup wizard:** open `https://<name>` (or `https://<ip>` if there is no DNS record
yet). Since the certificate is not trusted yet, the browser will show a warning — continue if the
fingerprint matches. Sign in as the administrator; the wizard asks for the server address and the
domain details (FQDN, target OU, GPO name).

**3. Domain — `tolikopa-setup.exe`:** run the tool downloaded at the end of the wizard on a
domain-joined computer, under an account that is allowed to create GPOs. It shows its plan and asks
for confirmation; it then copies the files to NETLOGON, creates the panel's **DNS record**, creates
and links the GPO, and uses the same GPO to make the panel's **certificate trusted on the
computers**. No domain password is ever entered into the panel.

**Updating:** `git pull && sudo ./setup.sh`. If the agent has changed, the panel's Setup page
offers `tolikopa-update.exe`. Back up `deploy/.env` and `deploy/tls/`.

Manual installation and details: [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md),
[`docs/GPO-DEPLOYMENT.md`](docs/GPO-DEPLOYMENT.md). To migrate a system installed under the old
name ("Gozcu"): [`docs/UPGRADING.md`](docs/UPGRADING.md).

## Documentation

| Document | Contents |
|----------|----------|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | System architecture, data flow, components |
| [`docs/AGENT.md`](docs/AGENT.md) | Agent design, collected metrics, **privacy allowlist** |
| [`docs/DATABASE.md`](docs/DATABASE.md) | TimescaleDB schema, hypertables, aggregates |
| [`docs/API.md`](docs/API.md) | REST + WebSocket + ingest contract |
| [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md) | Ubuntu server installation |
| [`docs/GPO-DEPLOYMENT.md`](docs/GPO-DEPLOYMENT.md) | Windows Server 2016 GPO rollout |
| [`docs/SECURITY.md`](docs/SECURITY.md) | Security + privacy + threat model |
| [`docs/DESIGN-SYSTEM.md`](docs/DESIGN-SYSTEM.md) | Panel design language, colours, animation |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | Release plan, milestones |

## License / Use

An authorized, defensive IT operations tool.
