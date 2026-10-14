# CLAUDE.md — Tolikopa (IT Fleet Monitoring)

**Tolikopa** is a privacy-respecting fleet monitor for an Active Directory domain: a small agent,
rolled out by GPO, reports system-health and general-usage metrics to a server on Ubuntu, and a web
panel shows the fleet.

The repository is meant to be public: **nothing organisation-specific belongs in it** — no
organisation name, real domain/NetBIOS name, IP, hostname, account or e-mail. Examples use `CORP` /
`corp.example.com` / `10.0.0.10`. Site-specific facts (production host, paths, credentials) live in
the gitignored `CLAUDE.local.md` and `deploy/.env`.

| Dir | What | Stack |
|-----|------|-------|
| `agent/`  | Agent (Windows service + Linux systemd) | Go, `gopsutil/v4`, `x/sys/windows/svc`, `wevtapi`, WMI |
| `server/` | Ingest + REST/WS API + alert engine | Go, Echo v4, `pgx/v5`, TimescaleDB |
| `web/`    | Admin panel SPA | Vite, React, TypeScript, Tailwind, `motion`, Recharts, TanStack Query |
| `setup/`  | `tolikopa-setup.exe` / `tolikopa-update.exe` — domain rollout tool an admin runs on their own PC | Go stdlib, GroupPolicy PowerShell module |
| `deploy/` | Compose, Caddy, GPO scripts | Docker Compose, Caddy, PowerShell |
| `setup.sh` | Server install/update: checks prerequisites (installs nothing), asks name/IP/admin, creates `.env` + self-signed cert in `deploy/tls/` | bash |
| `docs/`   | Technical documentation (English) | Markdown |

## The privacy rule (non-negotiable)

The agent collects only what the allowlist in `docs/AGENT.md` names; `agent/internal/model` is its
code form. Anything else is a bug, and widening the list means amending `docs/AGENT.md` first.

**Allowed:** CPU/RAM/disk gauges · NIC total byte counters · process **name + category + cpu% +
mem** · logged-on **username** · session state (active/idle/locked/…) · **seconds** since the last
keyboard/mouse input (a duration only) · foreground app **base name + duration** · disk SMART
health · app-crash events (faulting app **base name** + exception code + time) · presence of
**compiled-catalog** hygiene items only · the machine's **own local IPv4 addresses** (read from
interface configuration) · OS/CPU/RAM identity · battery state · Windows Update counts and times
(never update titles or KB numbers).

**Forbidden — never collect, transmit, log or store:** screen contents/screenshots · keystrokes ·
mouse coordinates · window titles or foreground-window text · URLs, browser history · DNS queries,
packet payloads · file names, file paths, document contents · process **command lines** ·
environment variables · open handles · per-process network connections · a general
installed-software inventory · OS error message strings.

In `gopsutil` process code call **only** `.Name()`, `.Times()`, `.MemoryInfo()`, `.CreateTime()` —
never `.Cmdline()`, `.Exe()`, `.Environ()`, `.OpenFiles()`, `.Connections()`, `.Cwd()`. The
`security-privacy-reviewer` subagent audits this.

## Conventions

- **Bilingual product (Turkish + English).** Every human-facing panel string goes through
  `web/src/i18n` (no literals in components; `en` is typed against `tr`, so a missing key fails the
  build). The API returns `{code, message, params?}` errors and alerts carry `rule` + `params`; the
  client does the wording. Setup-tool console text lives in `setup/cmd/tolikopa-setup/messages.go`.
  Title Case for nav, tabs, titles, buttons, table headers ("Alarm Eşikleri", "Crash Storm");
  sentence case for descriptions. Code, comments, commit messages, README and `docs/` are
  English.
- **Time:** store UTC (`timestamptz`); display and bucket rollups in `Europe/Istanbul`.
- **SQL:** the logged-on user column is `logon_user`, never `current_user`.
- **No secrets in the repo.** Passwords and tokens live only in `.env` (gitignored);
  `.env.example` holds placeholders.
- **Three independent Go modules** (`agent/`, `server/`, `setup/`) that never import each other.
  The wire contract is duplicated in `agent/internal/model` and `server/internal/model` and
  documented in `docs/API.md` — keep all three in sync by hand.
- **Pinned versions** (verify release notes before any major bump): Go 1.26 · gopsutil v4.26 ·
  Echo v4 · pgx v5 · `timescale/timescaledb:2.27.0-pg17` (core features only) · Caddy 2.11 ·
  Vite 8 · React 19.2 · Tailwind v4.3 · motion v12 (`import { motion } from "motion/react"`) ·
  Recharts 3.9 · TanStack Query v5 · React Router v7.

## Build & verify

Go is installed locally; Docker Desktop is often not running, so prefer the local toolchain.

```bash
cd agent && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/tolikopa-agent.exe ./cmd/agent
cd agent && GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/tolikopa-agent ./cmd/agent
cd server && go vet ./... && go test ./...
cd setup  && GOOS=windows go vet ./... && go test ./...
cd web    && npm run build
sudo ./setup.sh                                # full stack on Ubuntu (needs Docker)
```

- Agent changes: run `go vet` with **both** `GOOS=windows` and `GOOS=linux` — half the agent is
  behind `//go:build windows`. Two tests (`internal/config` TestNewLoggerWritesToFile,
  `internal/policy` TestStoreRoundTrip) fail on Windows for environmental reasons; pre-existing.
- **Versions:** the root `VERSION` file is the single source; the image build stamps it into the
  server, the setup tool and the agent. Bump it for a release; `agent/cmd/agent/main.go`'s default
  must equal it (a test checks).
- `agent/dist/`: `tolikopa-agent.exe` is the shippable build (the name `bootstrap.ps1` expects in
  `NETLOGON\Tolikopa`), `tolikopa-agent-prev.exe` the previous one for rollback. Rotate before
  building a new one.
- The api Docker image also compiles the agents and the setup tool; the panel serves them from
  there (`/api/admin/setup/download/...`).

## Wire/data contract (source of truth: `docs/API.md`, `docs/DATABASE.md`)

- Agent → `POST /api/v1/ingest`: gzip JSON `{machine, samples[], events[], disk_health[]}`,
  `Authorization: Bearer <ingest token>`, `Idempotency-Key: <uuid>`. Inserts are
  `ON CONFLICT DO NOTHING`; every hypertable unique key includes `ts`.
- Live fleet view and WebSocket read the hot `machine_live` table, **never** the hypertables;
  history comes from continuous aggregates.
- Panel auth: bcrypt + JWT in an httpOnly cookie, re-checked against `admin_user` on every request,
  CSRF token on writes, optional TOTP 2FA. Migrations are embedded and auto-apply at api start.

## Deploying

Host, paths and credentials: `CLAUDE.local.md` and `deploy/.env` — never in a committed file.
Server: `git clone` + `sudo ./setup.sh` (later: `git pull && sudo ./setup.sh`), see
`docs/DEPLOYMENT.md`. Agent rollout, DNS record and certificate trust: `tolikopa-setup.exe`, see
`docs/GPO-DEPLOYMENT.md`. `deploy/.env` and `deploy/tls/` are the installation's identity — a new
certificate breaks every agent's pin and the trust distributed to the PCs.

## Subagents (`.claude/agents/`, local only — `.claude/` is gitignored)

`windows-agent-dev`, `backend-api-dev`, `frontend-ui-dev`, `ui-designer`, `devops-deployer`,
`security-privacy-reviewer`.

## Safety

Authorized, defensive IT-ops software for machines the operator administers. Keep it transparent:
no covert collection, no remote command execution on endpoints, nothing that would surprise an end
user reading `docs/AGENT.md`.

**The one actuation carve-out (endpoint hygiene).** The agent may remove allowlisted bloatware and
disable allowlisted startup entries — desired-state reconciliation over a closed vocabulary, not a
command channel. All five invariants must hold; weakening one is a redesign, not a feature:

1. **The vocabulary is compiled into `tolikopa-agent.exe`** (`agent/internal/appcatalog`). The
   server can only select from it; anything else is refused as `unknown_item`.
2. **The server never supplies an executable, path, command line or argument** — only catalog item
   ids, and nothing server-controlled reaches `CreateProcess` argv, a registry path or a package
   identity without passing `appcatalog.Lookup`.
3. **`appcatalog.IsForbiddenFamily` is checked in both the parent and the removal worker.**
4. **Local kill switches win.** `allow_app_removal`, `allow_startup_control`, `enforce_actions`
   default to false, live in `config.json`, and cannot be set by the server (nor by a setup bundle).
5. **Every attempt is audited** (`remediation_attempt`); `error_code` is a bare code, never an OS
   message or a path.

**No shell in the agent:** no `powershell.exe`, no `os/exec` on Windows, no interpreter.

**Domain credentials never reach the server.** The server holds no domain account and cannot write
to NETLOGON or SYSVOL — otherwise a compromised server could replace `tolikopa-agent.exe` and run
code as SYSTEM on every PC. Rollout is done by `setup/`, run by a human admin under their own
session: it validates every manifest field against a strict pattern, writes only the three
allowlisted files to `NETLOGON\Tolikopa`, and refuses to modify a GPO it does not own.

**The panel certificate is a self-signed server certificate, never a CA.** It is distributed to
the domain's PCs as a trusted root, where a CA could vouch for any web site; a CA:FALSE leaf
vouches only for the panel's own name and IP. `setup/internal/domain/cert.go` refuses to distribute
anything else (CA flag or certSign usage, not self-signed, no serverAuth) — keep it that way.
The setup tool never overwrites an existing DNS record that points elsewhere.
