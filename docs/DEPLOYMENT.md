# Ubuntu Server Installation

Example target: `10.0.0.10`, panel name `tolikopa.corp.local`. Installation and updates are done
with a single script at the root of the repository: `setup.sh`.

## Prerequisites

`setup.sh` **installs nothing**. It first checks the server's requirements; if anything is
missing it stops and prints, in order, the commands to run to fix the gaps (for Docker, the steps
for Docker's official apt repository). Run the commands and start the script again.

`setup.sh` first asks for the language (Turkish / English); `--lang tr|en` skips the question.

What is checked:

| Requirement | Note |
|-------------|------|
| Docker Engine **≥ 24**, running | `docker info` must respond |
| Docker Compose plugin **≥ 2.20** | `docker compose version` |
| Docker Buildx **≥ 0.17** | `docker buildx version` |
| `curl`, `openssl`, `iproute2` (`ip`, `ss`), `systemd` | if missing, an `apt-get install` command is suggested |
| Ports 80 and 443 free | the process/container holding the port is shown by name (on an update run, this stack itself does not count) |
| **≥ 10 GB** free space in Docker's data directory | the build downloads several GB of images |
| Outbound access to Docker Hub, `proxy.golang.org`, `registry.npmjs.org` | the build downloads base images, Go modules and npm packages; if there is a proxy, configure it for Docker as well |

If memory is below 2 GB, only a warning is shown. The script is written for Ubuntu; on another
distribution it warns, and the commands it suggests are Ubuntu commands.

## Installation

```bash
git clone https://github.com/aliyalinkilic/tolikopa.git ~/tolikopa
cd ~/tolikopa
sudo ./setup.sh             # asks for the language first; or: sudo ./setup.sh --lang en
```

Asked on the first run:

1. **Panel DNS name** — e.g. `tolikopa.corp.local`. Browsers and agents reach the server by this
   name; the DNS record is created later by `tolikopa-setup.exe` (GPO-DEPLOYMENT.md).
2. **Server IP address** — the default is the address on the server's default route; Enter
   accepts it. If the address is not configured on any interface it warns (this may be correct
   behind NAT).
3. **First administrator** — username (default `admin`) and, twice and without echo, the password
   (at least 10 characters, at most 72 bytes).

A summary is shown and confirmation is requested. Then the script:

1. Generates a **self-signed** certificate in `deploy/tls/`: `tolikopa.crt` (644) +
   `tolikopa.key` (600), both owned by root; EC P-256, 3650 days, SAN = DNS name + IP,
   `CA:FALSE`, `serverAuth`. Why this rather than a CA: [SECURITY.md](SECURITY.md). A certificate
   already in `deploy/tls` (left by an interrupted first run, or restored from a backup) is
   reused if it is for the names given; for other names the script stops.
2. Writes `deploy/.env` (mode `600`): random `POSTGRES_PASSWORD`, `INGEST_TOKEN`, `JWT_SECRET`;
   `SERVER_HOST="<name> <ip>"`, `DEFAULT_SNI=<name>`. `.env` is written **after** the
   certificate on purpose: it marks an existing installation, so a first run that dies half-way
   is simply a first run again on the next start.
3. If `ufw` is **already enabled**, opens 80/443; if not, it does not touch the firewall. Note
   that ufw does **not** restrict Docker-published ports (see [Firewall](#firewall-and-published-ports)).
4. `docker compose up -d --build` — db, api, web (Caddy) are built and started; migrations are
   applied at API startup. The first time this can take 5–15 minutes.
5. Waits until `https://<name>/api/healthz` answers through Caddy, **verified against this
   certificate** (at most 5 minutes).
6. Creates the first administrator:
   `docker compose exec -T api /server admin init-admin <username>` — the password goes via stdin;
   it is not written to `.env`, the command line or the log. Skipped if the database already has
   an account.
7. Installs the Linux agent (the server monitors itself): `/opt/tolikopa/tolikopa-agent`, systemd
   unit `tolikopa-agent`, `/etc/tolikopa/config.json` (600) — `backend_url: https://<ip>`, the
   ingest token and this certificate as `tls_ca_pem`. The unit is sandboxed:
   `ProtectSystem=strict`, `ProtectHome=read-only`, `PrivateTmp=yes`, `NoNewPrivileges=yes`;
   the only writable path is `ReadWritePaths=/var/lib/tolikopa` (state, spool, logs). It still
   runs as root, because it reads every process's `/proc` entry and `statfs()`s every mount;
   `/proc`, `/sys` and `/run/systemd/sessions` remain readable, and `ProtectHome=read-only`
   (not `yes`) keeps a `/home` mount visible for disk usage.
8. Prints the panel address, the certificate's **SHA-256 fingerprint**, its expiry date and the
   next steps.

### Unattended installation

The questions can be answered with environment variables (no confirmation is asked). Preferred:
give the password in a file that only root can read (`TOLIKOPA_ADMIN_PASSWORD_FILE`; its first
line is the password):

```bash
sudo install -m 600 -o root -g root /dev/null /root/tolikopa-admin.pw
sudo nano /root/tolikopa-admin.pw            # type the password, save
sudo TOLIKOPA_HOST=tolikopa.corp.local TOLIKOPA_IP=10.0.0.10 \
     TOLIKOPA_ADMIN_USER=admin TOLIKOPA_ADMIN_PASSWORD_FILE=/root/tolikopa-admin.pw ./setup.sh
sudo shred -u /root/tolikopa-admin.pw
```

The script refuses the file unless it is a regular file (not a symlink) owned by root with no
group/other permissions (`600` or `400`). `TOLIKOPA_ADMIN_PASSWORD='...'` still works, but
`sudo VAR=... ./setup.sh` puts the password on sudo's command line (visible in `ps` while it runs)
and in the shell history.

## After installation

1. Open `https://tolikopa.corp.local` (or `https://10.0.0.10` if the DNS record does not exist
   yet). The browser **shows a certificate warning** — the certificate is not yet trusted on any
   computer. If the fingerprint shown matches the one `setup.sh` printed, proceed.
2. Sign in with the administrator account. The setup wizard (once per browser) opens at the
   "Server Address" step: server address and domain details (FQDN, target OU, GPO name) →
   `tolikopa-setup.exe`.
3. Run the tool on a domain-joined administrator computer: it deploys the agent, creates the
   panel's DNS record and makes this certificate trusted on the computers via GPO
   ([GPO-DEPLOYMENT.md](GPO-DEPLOYMENT.md)). From then on the warning disappears on computers the
   GPO applies to.

The wizard's setup code step is only for installations where no administrator was created on the
console (e.g. if `init-admin` failed). If `SETUP_CODE` is empty, the code is written to the API
log: `sudo docker compose logs api | grep -i 'setup code'`.

To view the certificate's fingerprint later:

```bash
openssl x509 -in deploy/tls/tolikopa.crt -noout -fingerprint -sha256
```

## Environment variables (`deploy/.env`)

Generated by `setup.sh`; `deploy/.env.example` documents the keys. The important ones:

| Variable | Description |
|----------|-------------|
| `POSTGRES_PASSWORD` | DB password (random) |
| `INGEST_TOKEN` | shared token used by the agents (random) |
| `JWT_SECRET` | session signing key (random) |
| `SETUP_CODE` | for the wizard's setup code step (if empty, the API generates one and writes it to its log) |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | optional; for compatibility with installations that predate the wizard |
| `SERVER_HOST` | `"tolikopa.corp.local 10.0.0.10"` — Caddy site addresses + CORS origin; same as the names in the certificate |
| `DEFAULT_SNI` | name of the certificate presented to clients that send no SNI (that connect by IP) |
| `OFFLINE_WARN_SEC` / `OFFLINE_CRIT_SEC` | offline thresholds |
| `SMTP_*` | e-mail notifications (off by default) |
| `BIND_ADDR` | optional; the host address 80/443 are published on (unset: all interfaces, IPv4 and IPv6) — see below |

## Firewall and published ports

Docker publishes container ports with its own iptables rules (the `DOCKER` chains), which are
evaluated **before** ufw's. A `ufw deny 443` therefore does **not** close the panel, and ufw rules
cannot narrow who reaches 80/443. Options:

- Bind to one interface: set `BIND_ADDR=10.0.0.10` in `deploy/.env` and run `sudo ./setup.sh`
  (compose then publishes `10.0.0.10:443:443`). The panel is then unreachable on the
  server's other addresses, loopback included (`setup.sh` probes the panel on `BIND_ADDR`). Use
  the server IP from the certificate: the agents — the server's own included — connect to it.
- Filter in the `DOCKER-USER` iptables chain (Docker leaves it to the administrator), or in the
  network firewall in front of the server.

Only the web container (Caddy) publishes ports; the database and the API are never published.

## Container hardening

All three services run with `no-new-privileges`. The API (user 65534) drops every Linux
capability (`cap_drop: [ALL]`); Caddy drops every capability except `NET_BIND_SERVICE` (to bind
80/443) and reads the TLS key as its owner — which is why `setup.sh` keeps `deploy/tls/*` owned by
root. The database keeps Docker's default capabilities: the postgres entrypoint starts as root,
fixes the data directory's ownership and drops to the `postgres` user.

Base images are pinned by tag and digest (`server/Dockerfile`, `web/Dockerfile`, the `db` image in
`deploy/docker-compose.yml`); Go modules come only from the committed `go.mod`/`go.sum`
(`go mod download` + `-mod=readonly`, no `go mod tidy` at build time) and npm packages only from
`package-lock.json` (`npm ci`). Updating a base image = replacing tag and digest
(`docker buildx imagetools inspect <tag>` prints the digest).

## TLS

Caddy serves the certificate generated by `setup.sh`
(`tls /etc/caddy/tls/tolikopa.crt /etc/caddy/tls/tolikopa.key`). `deploy/tls` is mounted
read-only into the web container only; no other container sees the key. Only the certificate is
mounted into the API container (`/tls/tolikopa.crt`, `TLS_CERT_FILE`): the panel puts it into the
agents' `config.json` (`tls_ca_pem`) and into the setup tool's bundle (`tolikopa.bundle` in `tolikopa-setup.zip`).

- **Domain computers:** `tolikopa-setup.exe` adds the certificate to "Trusted Root Certification
  Authorities" via GPO. Nothing needs to be done by hand.
- **An administrator PC outside the GPO's scope:** copy `deploy/tls/tolikopa.crt` and
  double-click it → "Local Machine" → "Trusted Root Certification Authorities".
- **The certificate must not change:** the agents pin it and the computers trust it. If
  `deploy/.env` exists but `deploy/tls` does not, `setup.sh` stops instead of generating a new
  certificate — restore the certificate from a backup.

## Verification

```bash
cd ~/tolikopa/deploy
sudo docker compose ps                     # three services running, db and api 'healthy'
curl --cacert tls/tolikopa.crt --resolve tolikopa.corp.local:443:127.0.0.1 \
     https://tolikopa.corp.local/api/healthz   # {"status":"ok"}
sudo docker compose logs -f api            # migrations + ingest logs
systemctl status tolikopa-agent            # the server's own agent
```

## Updating

```bash
cd ~/tolikopa && git pull && sudo ./setup.sh
```

When `deploy/.env` exists, `setup.sh` asks no questions: it checks the requirements, rebuilds the
images, restarts the services and refreshes the Linux agent. Migrations run automatically at
startup (idempotent). Updating the Windows agent is separate (GPO-DEPLOYMENT.md).

## Backup

Three things to back up:

1. **`deploy/.env`** — secrets (the ingest token is also on the agents).
2. **`deploy/tls/`** — certificate + key. If lost, a new certificate is needed, and it must be
   redistributed to all agents and the GPO.
3. **The database:**

```bash
cd ~/tolikopa/deploy
sudo docker compose exec -T db pg_dump -U tolikopa -d tolikopa -Fc > tolikopa.dump
```

(`-T` is required: a TTY corrupts the binary dump.) Follow the TimescaleDB dump/restore notes so
that CAggs + policies are preserved on restore; do a restore test before go-live.

## Troubleshooting

- **`setup.sh` stops with "Cannot continue — missing:"** → run the commands it prints in order
  and start the script again.
- **"No certificate in deploy/tls"** → `deploy/.env` exists but the certificate does not. Restore
  `deploy/tls/` from a backup. If you really are installing from scratch, remove `deploy/.env`
  too; a new certificate and token require removing the old GPO and running
  `tolikopa-setup.exe` again. (An older `setup.sh` wrote `.env` before the certificate, so an
  interrupted first run of it leaves exactly this state; if no agent was ever deployed, just
  remove `deploy/.env`.)
- **"deploy/tls holds a certificate that is not for …"** → a first run found a certificate for
  other names. Restored from a backup: give the same names. Left over from an aborted install
  that was never distributed: remove `deploy/tls` and run again.
- **The browser shows a certificate warning** → expected if the GPO has not been applied yet.
  Compare the fingerprint with the `openssl` command above and proceed. Permanent fix:
  `tolikopa-setup.exe` (or a manual import). The certificate also covers the IP when you connect
  by IP; if you connect with a different name, the warning remains.
- **"The panel did not answer within 5 minutes"** → `sudo docker compose logs api web`.
- **The panel does not open** → `sudo docker compose logs web` (Caddy); the network firewall;
  `BIND_ADDR` in `.env` (if set, the panel answers only on that address).
- **Caddy restarts with `open /etc/caddy/tls/tolikopa.key: permission denied`** → the key is not
  owned by root (e.g. restored as another user). `sudo ./setup.sh` fixes ownership, or
  `sudo chown root:root deploy/tls/*`.
- **An agent is not sending data** → `sudo docker compose logs api` (401/429); check the token and
  the certificate pin (`tls_ca_pem`).
- **DB is not `healthy`** → `sudo docker compose logs db`; disk space; the `pgdata` volume.
