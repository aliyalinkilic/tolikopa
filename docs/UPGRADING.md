# Upgrade notes

## Migrating a system installed under the "Gozcu" name to Tolikopa (one-time)

The product's former name also appeared in runtime identifiers: the Windows service, the
`C:\ProgramData` folder, the NETLOGON path, the scheduled task, the compose project name and the
session cookies. In addition, old installations were set up with `deploy/ubuntu-bootstrap.sh` and
used Caddy's internal CA (`tls internal`) for TLS; these have been replaced by `setup.sh` and the
self-signed certificate under `deploy/tls/` (DEPLOYMENT.md). There is nothing to do for new
installations. For an installation running under the old name there are two paths.

### Path A — clean reinstall (recommended if history is not needed)

Simpler: the old system is removed and `setup.sh` installs from scratch.

```bash
cd <old-project>/deploy
sudo docker compose exec -T db pg_dump -U gozcu -d gozcu -Fc > ~/gozcu.dump   # backup (user/DB name same as in .env)
sudo docker compose down -v                                                      # containers + volumes are deleted
sudo systemctl disable --now gozcu-agent 2>/dev/null; sudo rm -f /etc/systemd/system/gozcu-agent.service
sudo rm -rf /var/lib/gozcu /etc/gozcu /opt/gozcu
```

Then clone the new repository and run `sudo ./setup.sh` (DEPLOYMENT.md). A reinstall generates
**a new TLS certificate and a new ingest token**; old agents recognise neither. So on the Windows
side, **remove the old GPO** and run `tolikopa-setup.exe` from a freshly downloaded `tolikopa-setup.zip` again (the
migration in section 2 below works exactly the same with the new `config.json`). Machines appear
in the panel as new records.

### Path B — in-place migration (history is kept)

#### 1. Server (**before updating**, to avoid data loss)

Docker prefixes named volumes with the project name (`gozcu_pgdata`). If the project name
changes, compose starts with an empty database. Pin the old name in `.env`:

```bash
cd <project>/deploy
grep -q '^COMPOSE_PROJECT_NAME=' .env || echo 'COMPOSE_PROJECT_NAME=gozcu' | sudo tee -a .env
grep -E '^POSTGRES_(USER|DB)=' .env      # both must be defined (old default: gozcu)
```

If the `POSTGRES_USER` / `POSTGRES_DB` lines are missing, add them with their old values
(`gozcu`); compose's default is now `tolikopa`.

**TLS.** If `deploy/.env` exists but `deploy/tls/` does not, `setup.sh` does not generate a new
certificate; it stops. The old installation's certificate was in Caddy's internal CA and is not
carried over; create the certificate once by hand, with the same properties as the one `setup.sh`
generates (replace the name and IP with your own values):

```bash
cd <project>
sudo install -d -m 0700 deploy/tls
cat > /tmp/tolikopa-cert.cnf <<'EOF'
[req]
distinguished_name = dn
prompt = no
x509_extensions = ext
[dn]
CN = tolikopa.corp.local
O = Tolikopa
[ext]
basicConstraints = critical, CA:FALSE
keyUsage = critical, digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = DNS:tolikopa.corp.local, IP:10.0.0.10
subjectKeyIdentifier = hash
EOF
sudo openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 3650 \
  -config /tmp/tolikopa-cert.cnf -keyout deploy/tls/tolikopa.key -out deploy/tls/tolikopa.crt
sudo chmod 600 deploy/tls/tolikopa.key && sudo chmod 644 deploy/tls/tolikopa.crt
rm /tmp/tolikopa-cert.cnf
```

Use the same names in `deploy/.env` — `setup.sh` reads the first word of `SERVER_HOST` as the
name and the second as the IP:

```
SERVER_HOST="tolikopa.corp.local 10.0.0.10"
DEFAULT_SNI=tolikopa.corp.local
```

The server's own Linux agent (if installed):

```bash
sudo systemctl disable --now gozcu-agent
sudo mkdir -p /var/lib/tolikopa
sudo cp -a /var/lib/gozcu/. /var/lib/tolikopa/      # agent_id is kept → same machine record
sudo rm /etc/systemd/system/gozcu-agent.service
```

Then update: `sudo ./setup.sh` (at the repository root). Because `deploy/.env` exists it asks no
questions; it builds and starts the stack, migrations are applied at startup, and it writes
`tolikopa-agent.service` and `/etc/tolikopa/config.json` (with the new certificate). Then delete
the old folders: `sudo rm -rf /var/lib/gozcu /etc/gozcu /opt/gozcu`. Because the cookie names
changed, panel users sign in again once.

Since the certificate changed, old agents (which pin the old Caddy root CA) cannot send reports
until the new `config.json` reaches them; measurements wait in the queue. Do the steps below
right after the server update.

#### 2. Windows fleet

The new `bootstrap.ps1` performs the migration itself on each PC: if the old **service** is
installed it stops and removes it, moves the machine identity (`state\agent_id`) under
`C:\ProgramData\Tolikopa` (the machine stays the same device in the panel), and deletes the old
local task and folder. The unsent queue (`spool`) is not carried over; at most a few minutes of
measurements are lost. If the old folder's owner is not SYSTEM or Administrators, nothing is taken
from it and the folder is left untouched.

1. Download `tolikopa-setup.zip` from the panel, extract it into one folder and run
   `tolikopa-setup.exe` from there: it puts the new `tolikopa-agent.exe`,
   `bootstrap.ps1` and the `config.json` **carrying the new certificate** under
   `\\<domain>\NETLOGON\Tolikopa\`, creates the panel's DNS record, and creates and links the new
   GPO (task name `TolikopaAgent`) together with the certificate trust (GPO-DEPLOYMENT.md). Target
   the OU the old GPO is linked to.
2. **Unlink the old GPO or delete it.** As long as the old GPO stays linked, the old task
   reinstalls the old service on every refresh and the two agents take turns removing each other.
3. Once the fleet has moved to the new agent (tracked via the agent version in the panel), delete
   the `\\<domain>\NETLOGON\Gozcu` folder.

The migration block is in `deploy/gpo/bootstrap.ps1` under the heading "Gecis (tek seferlik)";
it can be removed once no agent under the old name remains in the fleet.
