# Security and Privacy

## Privacy principle

Tolikopa collects **only aggregate system/usage metrics**. No data is collected that shows what a
person is doing on their screen. The binding allowlist: [`AGENT.md`](AGENT.md). This is not a
"feature" but an architectural constraint — if the code violates this list, that is a bug.

## Threat model (brief)

Trusted: the LAN, the domain, the server administrator. Risks addressed: a compromised client
leaking the ingest token; network eavesdropping; unauthorised access to the panel; the agent being
abused to covertly collect data (prevented architecturally).

## Agent identity (ingest token)

- **Default:** a single ingest token shared by all clients (to keep GPO distribution simple).
- **Hardening (implemented):**
  - Sent only over TLS.
  - Kept in plain text in `deploy/.env` (mode 600) and in the api container's environment, because
    the server must put it into the agents' `config.json` and the setup tool's bundle; incoming
    requests are compared against it in constant time. Rotatable (`.env` → `INGEST_TOKEN`), after
    which the bundle must be redistributed.
  - **LAN IP allowlist** in Caddy for `/api/v1/ingest`: all RFC 1918 private ranges
    (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) + loopback. So it is a "not from the
    internet" filter, not "our own subnet"; every address on the same private network can reach
    the ingest endpoint.
    (Rate limiting is **not implemented** — the stock `caddy:2.11` image has no `rate_limit`
    directive; it requires a custom build. The only back-pressure is the pool-saturation 429 in
    `ingest.go`.)
  - Ingest-scope only; it cannot access any other endpoint.
  - On the client, the `config.json` ACL is SYSTEM+Administrators only.
  - On the distribution share (`NETLOGON\Tolikopa`) the folder ACL is narrowed by the setup tool
    (below) — ordinary domain users cannot read the token.
- **Upgrade path (documented):** per-machine token (the server generates a unique token via
  automatic enrollment on first contact). For a small internal network, a shared token + allowlist
  is sufficient.

### What the ingest token can and cannot do in the endpoint hygiene feature

Endpoint hygiene (bloatware removal / disabling startup entries) is a destructive capability; the
limits of the shared ingest token with respect to this capability:

**What the token does:** it authenticates only in the **agent → server** direction. Its holder can
call `POST /api/v1/ingest`, i.e. write a *fake machine report* (made-up hostname, made-up presence
list, made-up attempt record). This is a data-integrity problem, not a privilege escalation.

**Who can read the token.** The token sits in `config.json` in two places: on every PC (ACL:
SYSTEM + Administrators only) and on the distribution share, `\\<domain>\NETLOGON\Tolikopa\config.json`.
NETLOGON is readable by **every authenticated user** by default; so `tolikopa-setup.exe` narrows the
ACL of the `Tolikopa` folder (inheritance disabled) **before** writing the files: **Domain
Computers** read; **Domain Admins**, **SYSTEM** and **Administrators** full control. This way
computer accounts (the startup script runs as SYSTEM) can read the token, ordinary domain users
cannot.

Honest limits: (1) In an installation where the share was set up **by hand** (files copied without
the tool) and this ACL was not applied, the token is open to **anyone** who can sign in to the
domain — in that case the token must be considered exposed, the ACL fixed and the token rotated.
(2) The ACL grants read to computer accounts; anyone who is SYSTEM/local administrator on a PC can
still read the token. The shared-token model does not solve this; the solution is a per-machine
token.

⚠️ **But the audit trail inherits the same trust level.** `machine_id` is not bound to the token;
the value in the body is accepted as is. So **a single** compromised PC (or any user able to read
a share without the ACL) can read the token from `config.json` and, with another machine's
`machine_id`, write
`{"item_id":"mail_calendar","action":"remove","outcome":"ok"}`. The panel then shows Tolikopa
removing an app on a machine it never touched, and the fake row is indistinguishable from a real
one — and on top of that `remediation_attempt` is retained for 365 days. A "data-integrity problem"
was acceptable for metrics; **it is not for the audit record of a destructive capability.** The
permanent fix is the "upgrade path" above: a per-machine enrollment token. Until that arrives,
these records are an indicator, not forensic evidence.

**What the token cannot do:** it cannot **push** policy. Policy flows the other way,
**server → agent**, and is produced only in the server's ingest response; the only way to change it
is to sign in to the panel with the `admin` role and call `PUT /api/admin/remediation/policy`
(cookie + CSRF + the JWT `Subject` is written to `remediation_policy_audit`). The token holder
cannot reach any other endpoint (ingest-scope only, LAN allowlist in Caddy; no rate limit).

**What happens if the server is compromised:** the blast radius is **limited to the compiled
allowlist**. The server can only **select from** the `item_id`s present in the agent's binary;
nothing outside the list can be requested — an unrecognised item is rejected locally and reported
back as `outcome:"unknown_item"`. Therefore remote command execution, removal of arbitrary packages
or touching an arbitrary file is not possible through this channel; extending the vocabulary
requires distributing a new `tolikopa-agent.exe` through the audited GPO path. In addition, every
machine has a GPO-provided local kill switch (`allowed.apps/startup/enforce`): a machine with
`enforce=false` only evaluates and reports, whatever the panel says. The server-side `dry_run`
(default **on**) and `enabled` (default **off**) flags are also independent vetoes — the most
restrictive one wins.

**Privacy boundary:** the agent reports only the presence of allowlisted items; this is not an
installed-software inventory. `error_code` is only a code (`0x80073cfa`) — an operating-system
message or file path is never transmitted.

## Administrator identity

bcrypt (cost 12) + JWT (HS256). The token is in an httpOnly + Secure + SameSite=Lax cookie. CSRF
token (`X-CSRF-Token`) on write requests. The first administrator is normally created on the server
console by `setup.sh` (`server admin init-admin`, see "First administrator" below); if it was not
created on the console, it is created from the browser with the setup code or — if provided —
seeded from `ADMIN_USERNAME`/`ADMIN_PASSWORD` in `.env`; **change the seeded password at first
sign-in** (panel → Profile → Change Password).

**Seeding from the environment happens only while the `admin_user` table is completely empty**
(under the same advisory lock as the setup code and `init-admin`; only one of them can create the
first account). The old rule was "create it if this username does not exist"; since accounts can
now be deleted, this brought a deliberately deleted shared `admin` account back at the next
restart, with the `.env` password and without 2FA. Now environment variables do **not** touch an
installation that has any account at all; the old `ADMIN_PASSWORD` line in `.env` can (and should)
be deleted.

Panel usernames are unique case-**insensitively**: the API rejects duplicates on creation and
migration 0016 builds a unique index on `lower(username)`. If accounts differing only by case
already exist, the index is not built (so the migration does not fail on a live database), the
server writes a warning to the log on every start, and builds the index itself once the conflict
is resolved.

### Password rule

At least **10 characters**, at most 72 bytes (the bcrypt limit — longer ones are rejected rather
than silently truncated), different from the current password. The rule is enforced both on the
user's own change and on an administrator's create/reset operations. (The `ADMIN_PASSWORD` seed at
startup is exempt from the rule; an installation can be done with a short value — change it.)

### Sessions and revocation

The JWT is valid for 12 hours but is **not trusted on its own**. On every request a single row is
read from `admin_user` and the token is rejected when:

- the account has been deleted, or re-created with the same name (the token carries
  `admin_user.id`);
- the account is disabled;
- `session_epoch` differs from the one in the token;
- the token has been **revoked by sign-out** (its token ID `jti` is in the `revoked_session`
  table). Sign-out revokes only that token; a stolen/copied cookie is also useless after sign-out.

`session_epoch` is incremented on these events: the user changing their password, an administrator
resetting the password, the account being disabled, 2FA being enabled, the emergency commands.
Result: the user's REST requests are rejected **on every device at the next request**. A user who
changes their own password gets a new cookie in the current tab; the others drop. The **role** is
read from the database, not from the token — a demotion takes effect at the next request.

A **WebSocket** connection is long-lived and cannot be checked per request; there are three
separate mechanisms for it, and the real guarantee is this:

1. **On upgrade** the same check as REST is performed; it is performed **once more after** the
   connection is registered with the hub (so a revocation falling between the first check and the
   registration is not missed).
2. A revocation coming from **the same server process** (password change/reset from the panel,
   disabling, deletion, enabling 2FA, resetting 2FA, turning on "Require Two-Factor
   Authentication") closes the user's open connections **immediately**.
3. Every connection is re-validated **every 60 seconds** (token expiry + the same user-row query)
   and closed if that fails. This covers the cases the process cannot see: token expiry and a
   revocation made **from another process** (emergency commands, manual database intervention) and
   **sign-out** (an open socket in another tab for the signed-out token). So in these cases a
   socket outlives its session by **at most ~60 s** — not "instantly".

The server performs these closures with code `1008`. On a `1008` (or a socket that never opens),
the panel asks for the session via `/api/auth/me` before reconnecting; if the session is gone it
switches to the sign-in screen, and if mandatory 2FA enrollment is required, to the enrollment
screen.

Known limit: if the re-validation query itself cannot run (database unreachable), the connection
is not dropped; it is retried on the next round; with no database, no fleet data flows either. The
token expiry is still enforced in this case (it is checked locally before the query).

If the "Require Two-Factor Authentication" setting cannot be read (database error) the request gets
**500**; the setting is not silently treated as "off", nor does it continue with a stale value.

On upgrading to this version, old cookies (lacking the new `aud`/`uid` fields) are invalid;
everyone signs in again once.

### Two-step verification (2FA / TOTP)

- **Algorithm:** RFC 6238 — HMAC-SHA1, 6 digits, 30 s, ±1 step tolerance. Written with the
  standard library (no extra dependency), verified against the RFC's test vectors. Works with any
  authenticator app (Google/Microsoft Authenticator, Aegis, 1Password, …).
- **Setup asks for the password:** `/api/me/2fa/setup` hands out the secret only with the current
  password. A stolen session cookie alone cannot bind an attacker's authenticator to an account
  that has no 2FA yet.
- **No reuse:** the time step of an accepted code is recorded; a code for the same or an older step
  is not accepted a second time, even within the validity window (a code seen over the shoulder or
  phished is useless). The record is made with a conditional `UPDATE`: of two concurrent requests
  carrying the same code, only one passes.
- **Secret storage:** the TOTP secret is encrypted in the database with **AES-256-GCM**. The key is
  derived from `JWT_SECRET` with HKDF-SHA256 (with the label `tolikopa/totp-secret/v1`); the
  ciphertext is bound to the account's `id` (AAD) and does not decrypt if copied to another row. A
  database backup alone does not yield the secrets.
- **Setup is two steps:** `setup` only produces a *pending* secret; 2FA is enabled when `enable`
  proves with a code that the authenticator really holds that secret. An unfinished setup locks
  nothing.
- **Recovery codes:** 10 of them, `xxxxx-xxxxx` (50 bits from the operating system's CSPRNG).
  Shown **only once**; the database holds only keyed digests (HMAC-SHA256, key derived from
  `JWT_SECRET`). Each code is **single-use**, compared in constant time, and deleted when used.
  Regenerating invalidates all the old ones.
- **Sign-in flow:** a correct password on a 2FA account does **not** issue a cookie; it issues a
  5-minute `mfa_token` valid only for the second step. This token cannot be used as a session
  (separate `aud` **and** separate signing key) and becomes invalid if the account's
  password/status changes in the meantime.
- **Disabling / regenerating codes** requires password + code. An administrator can reset another
  user's 2FA (`reset-2fa`); they **cannot** reset their own 2FA or password through the admin
  endpoints — so a compromised admin session cannot bypass re-authentication and take over the
  account.

⚠️ **If `JWT_SECRET` is rotated, all enrolled 2FA becomes invalid.** TOTP secrets can no longer be
decrypted and recovery-code digests no longer match (and all sessions drop as well). Users with 2FA
cannot sign in; each one needs a `reset-2fa` (by an administrator from the panel, or, if nobody can
get in, with the emergency command below) and 2FA is set up again. Plan for this before rotating
`JWT_SECRET`; do not rotate the secret except on suspicion of a leak.

### The "Require Two-Factor Authentication" policy

`require_2fa` in the general settings applies to all panel accounts. By design it **cannot lock
anyone out**: a user without 2FA signs in with their password, but until they set up 2FA they can
only reach the setup endpoints (everything else gets `403 mfa_enrollment_required`; WebSocket
included). Once setup is done the restriction lifts without needing to sign in again. To turn the
policy on, the administrator's **own** 2FA must be enabled; while the policy is on, nobody can
disable their own 2FA. Details: docs/API.md §2.4.

Limit: an account in the mandatory-enrollment stage is protected **only by the password** (the
person who sets up 2FA at first sign-in is whoever knows the password). After creating a new
account, verify that the user has finished setup via the `totp_enabled` column in the panel's user
list.

### Attempt limiting (rate limit)

`/api/auth/login` and `/api/auth/login/mfa` are counted per (username, client IP): after **5 failed
attempts** the key is locked; the lockout duration grows with each lockout (1 min → 5 min →
15 min → 1 h). While locked, even the correct password is rejected (`429` + `Retry-After`). The
password step and the code step are counted separately — a correct password does not reset the
code-guessing budget. Endpoints that ask for a password/code while signed in (`/api/me/password`,
`/api/me/2fa/*`) are subject to the same limit.

**The second step is also counted per account (independent of IP).** With only a (user, IP)
counter, someone who had obtained the password and could change their LAN address would get 5 code
attempts from each address. Every place that asks for a verification code (`/api/auth/login/mfa`,
`/api/me/2fa/enable`, `/api/me/2fa/disable`, `/api/me/2fa/recovery-codes`) also writes a wrong code
to the **account's** counter; after 5 wrong codes that account's second step is locked **from every
address** with the same growing durations. A deliberately accepted cost: someone who knows the
password can keep the real user's second step locked. That person already holds half of the
credentials; the remedy is to change the password — a password change/reset clears this lock (an
administrator can reset it from the panel; a server restart also clears it).

- Counters are **in memory**: they reset when the server restarts; sufficient for a single-process
  installation.
- The client IP is the first address after the trusted proxy in the `X-Forwarded-For` chain (not
  the value the client wrote).
- The **password** step's limit is specific to the (user, IP) pair: an attacker who does not know
  the password cannot lock the real user out at their own address by trying from another address.
- The password step is also counted **per address** (independent of username): after 20 wrong
  passwords from one address, that address is locked with the same growing durations. This also
  slows down someone trying one password against many accounts; no account gets locked. A correct
  password resets the address's counter.
- The username matches case-insensitively at sign-in (names are already unique this way).
- For an unknown username a real, costly bcrypt comparison is also performed (against username
  discovery by timing); the "account disabled" response is given only after a correct password.

### Emergency commands (if the last administrator is locked out)

For the last administrator who has lost their phone **and** recovery codes, or forgotten their
password, the server binary has subcommands that write directly to the database. They need only
`DATABASE_URL`, work even while the HTTP server is down, and print a one-line result (success:
exit code 0):

```bash
cd deploy
# Reset 2FA (the user signs in with their password and sets up 2FA again)
sudo docker compose exec api /server admin reset-2fa <user>

# Reset the password — the new password is read from STDIN, never from an argument
sudo docker compose exec api /server admin reset-password <user>     # prompts; input is VISIBLE on screen
printf '%s\n' "$YENI_SIFRE" | sudo docker compose exec -T api /server admin reset-password <user>

# Turn off the "Require Two-Factor Authentication" policy (the running server applies it within 15 s at most)
sudo docker compose exec api /server admin require-2fa off
```

- `reset-2fa` and `reset-password` increment the user's `session_epoch`: all their open sessions
  drop.
- `reset-password` enforces the password rule (≥10 characters); it never writes/logs the password
  anywhere. Do **not** pass the password as a command-line argument (it is not accepted): it would
  remain in `ps` output and shell history. In the second form, assign the variable in a way that
  does not end up in history (`read -rs YENI_SIFRE`).
- `require-2fa` accepts only `off`; turning it on is done from the panel (requires an
  administrator whose own 2FA is enabled).
- Whoever can run these commands already has access to the container and the database
  credentials; they grant no new privilege. Protect shell access to the server accordingly.

### Logging

Passwords, TOTP codes and secrets, recovery codes, `mfa_token` and session tokens are **never**
logged. Account management operations (creation, role/status change, password and 2FA reset,
deletion, `require_2fa` change) are written to the server log in who–whom form (there is no
persistent audit table yet).

## Initial setup and domain rollout

### First administrator: console (`init-admin`)

`setup.sh` asks for the first administrator's username and password on the server console and
creates the account with `docker compose exec -T api /server admin init-admin <user>`:

- The password is read **from stdin**; it is not written to the command line, `.env` or any log.
  The password rule (≥10 characters, ≤72 bytes) is enforced.
- Unattended installs should pass it as `TOLIKOPA_ADMIN_PASSWORD_FILE` — a regular file owned by
  root with no group/other permissions; `setup.sh` refuses anything else. The plain
  `TOLIKOPA_ADMIN_PASSWORD` variable still works, but `sudo VAR=... ./setup.sh` exposes the value
  on sudo's command line (`ps`) and in the shell history. Either way `setup.sh` unsets the
  variable so nothing it starts inherits it.
- The command works only **while there is no panel account at all** (under the same advisory lock
  as the setup code); afterwards it refuses. So it is not a way to add a second administrator to an
  existing installation.
- This way, on a fresh installation, a sign-up form that could claim the panel is never left open
  on the network.

### First administrator: setup code (if not created on the console)

In an installation where no administrator was created on the console (e.g. `init-admin` failed or
the stack was installed outside `setup.sh`) there is no panel account, and the first administrator
is created from the browser (`POST /api/setup/admin`). To prevent the first person to reach the
port from claiming the panel, this operation requires a **one-time setup code**:

- The code is only where the person who installed the server can see it: the `SETUP_CODE`
  environment variable (≥8 characters) or — if not provided — a 16-character (80-bit) code
  generated at startup and written to the **server log**:
  `sudo docker compose logs api | grep -A8 "FIRST-RUN SETUP"`.
- Only its digest is kept in memory; it is compared in constant time and never written to the
  database. The generated code changes on every restart; it becomes invalid the moment the account
  is created.
- Guessing attempts are subject to the same growing lockout as sign-in, per client IP.
- The endpoint works only while the `admin_user` table is **empty** (a single step under an
  advisory lock); afterwards `409 setup_already_done`. Existing installations (those with an
  account) are not affected at all: no code is generated.
- The generated setup code is the **only** secret this server writes to the log, and it is
  worthless once the account exists. A code provided via `SETUP_CODE` is not written to the log.
  `ADMIN_USERNAME`/`ADMIN_PASSWORD` seeding continues to be supported optionally (only while the
  table is empty — above).
- **If you wrote a `SETUP_CODE` into `.env` by hand, delete it after the first administrator is
  created** (`setup.sh` writes this line empty). The generated code changes on every start and dies
  once the account exists; but a **fixed** code sitting in `.env` is **re-armed** at server startup
  whenever the table becomes empty again for any reason (database reset, restoring an empty
  backup), and anyone who knows that code can claim the panel.

### Why the server holds no domain credentials

Rolling the agent out to the domain (copying files to `\\<domain>\NETLOGON\Tolikopa`, creating and
linking the GPO) requires domain administrator rights. Giving these rights to the server would turn
a monitoring panel into **an attack target for the entire domain**: compromising the panel or a
database backup would directly mean domain admin. Therefore:

- The server has **no field, table or endpoint** for a domain username/password, and none must be
  added.
- The rollout is done by `tolikopa-setup.exe`: the administrator downloads it from the panel and
  runs it on **their own** domain PC, under **their own** session. The tool does not ask for,
  store, or send a password anywhere, and makes no network connection to the Tolikopa server; it
  acts only with the rights the person running it already has.
- The server's contribution is to append an installation-specific bundle (manifest + agent +
  `bootstrap.ps1` + `config.json`) to the end of the generic exe. The tool re-validates every
  manifest field before using it (narrow regexes; no quotes, `$`, `;` or backticks in values passed
  as arguments to PowerShell), accepts only a closed set of file names, and checks each file
  against its SHA-256. The panel enforces the same rules when saving. `domain_fqdn` cannot look
  like an IP (a name whose last label is digits only — `10.0.0.99`, `corp.123` — is rejected on
  both sides): the value goes into a UNC path.
- **Before** touching the network, the tool compares the domain of the computer it runs on with
  `domain_fqdn` in the manifest (it cannot be redirected to another domain or to a server of the
  attacker's choosing) and pins all GPO operations to the **PDC emulator**.
- The tool touches only a GPO **it created itself**: it creates the GPO with its own marker in the
  description field; it refuses to modify **or remove** a GPO that has the same name but lacks
  this marker. An existing GPO (e.g. Default Domain Policy) cannot be targeted via `gpo_name` in
  the manifest.
- Before writing the files it narrows the ACL of the `NETLOGON\Tolikopa` folder (see "Who can read
  the token" above).
- When adding the panel's DNS record it does **not overwrite** an existing record: if the name
  points to another address (or a CNAME/AAAA exists) it stops. The DNS name must be one of the
  names covered by the panel certificate. A name that no existing zone holds would need a new
  zone (which answers for that whole name space): that is only done after the admin types the
  zone name interactively; `-yes` / `-dry-run` refuse it, and `gpo.ps1` refuses to create a zone
  unless the exe passes the confirmation.
- It adds the certificate to the trusted roots via GPO only if it is a self-signed server
  certificate that is not a CA (see "TLS" below); `-remove` removes this trust together with the
  GPO.

### What a compromised server can and cannot do now

**Cannot:** do anything in the domain on its own. It holds no credentials; it cannot send commands
to endpoints (the hygiene channel is limited by the compiled allowlist — above); it cannot turn on
the agents' local kill switches: the `config.json` it produces **never contains** the
`allow_app_removal`, `allow_startup_control`, `enforce_actions` and `hygiene_hours` keys (pinned
by a test). The setup tool asks the administrator **locally** for the `allow_app_removal` /
`allow_startup_control` / `enforce_actions` values; on later runs (including updates) it
**preserves** whatever the existing `config.json` on the share says — these values are never taken
from the bundle (`setup/internal/domain/localconfig.go`).

This protection has two layers, because its first form could be bypassed: the tool used to take
the bundle's `config.json` and merge it with a "delete the four forbidden keys" approach, and a
security review showed that a key such as `"allow_ſtartup_control"` (long s, U+017F) slipped past
the delete list and landed on the real key in the agent's case-insensitive JSON matching. Now:

1. The **tool** **builds** the `config.json` it will write to the share by taking from the bundle
   only the keys in a **closed allowlist** (`backend_url`, `ingest_token`, `tls_ca_pem`,
   `intervals`, `top_n`, `spool_max_mb`, `log_level`); no key outside the list — however it is
   spelled — gets through.
2. The **agent** refuses to load a `config.json` in which one of the keys matches a kill-switch
   name under case folding but is **not byte-for-byte identical** to it.

**Can:** **serve a malicious bundle** to an administrator. The downloaded `tolikopa-setup.zip` /
`tolikopa-update.zip` comes from the server; someone who compromises the server can replace the exe
or the agent inside it, and if the administrator runs it with domain admin rights, the domain is
taken over. This is the unavoidable residual risk of the "download software from the server and
run it" model; mitigations:

- **The tool prints its plan**: it shows what it will copy where, which GPO it will link where, and
  the **SHA-256** of the agent it will distribute, and asks for confirmation (`-dry-run` changes
  nothing). It checks every file in the bundle against the SHA-256 in the manifest.
- **The panel shows the agent's SHA-256** (`GET /api/admin/setup` → `artifacts.agent_sha256`).
  Before confirming, the administrator **compares the value printed in the tool's plan with the one
  shown in the panel**; optionally they can also verify it with `Get-FileHash` on the
  `tolikopa-agent.exe` copied to the share and — if in doubt — against a binary they built
  themselves from source. (If the server is fully compromised, the value shown by the panel cannot
  be trusted either; the independent source of verification is the repository/build output.)
- Downloads are open only to the `admin` role, served with `no-store`, and each one is logged
  (who, which file, from which IP).
- The exe in the download is the build artifact **unmodified** (the bundle is the separate
  `tolikopa.bundle`), so its SHA-256 is the same for every installation of a build and can be
  compared against a published or self-built value.
- Signing the binaries with Authenticode is the step that actually closes this risk (below). The
  download layout already allows it: nothing is appended to the exe, so a signature would survive.

**Ingest token.** It leaves the server only inside `config.json` (inside the bundle and as a
standalone download); it does not enter the logs, the manifest, or the `GET /api/admin/setup` body
(it is masked there). The downloaded bundle must therefore be treated as **secret**: whoever
obtains it can write fake machine reports (the "Agent identity" section above). `server_url` can
only be `https` — the token cannot be configured to be sent over plain HTTP.

## Container boundaries

- The API container runs as a **non-root** user (65534) with **every** Linux capability dropped
  (`cap_drop: [ALL]`).
- Caddy (web) drops every capability except `NET_BIND_SERVICE` (it binds 80/443 as root inside the
  container); it reads the key as its owner, so `setup.sh` keeps `deploy/tls/*` owned by root.
- All three services run with `no-new-privileges` (no gain through setuid binaries or file
  capabilities). The database keeps Docker's default capability set: the postgres entrypoint
  starts as root, fixes data-directory ownership and switches to the `postgres` user.
- Only the web container publishes ports. **Docker-published ports bypass ufw**: Docker's iptables
  rules are evaluated before ufw's, so a ufw rule does not restrict who reaches 80/443. Restrict
  with `BIND_ADDR=<server IP>` in `.env` (publishes on that address only), the `DOCKER-USER`
  iptables chain, or the network firewall ([DEPLOYMENT.md](DEPLOYMENT.md#firewall-and-published-ports)).
- The TLS private key (`deploy/tls/tolikopa.key`) is mounted **only** into the web (Caddy)
  container, read-only. Only the certificate file is mounted into the API
  (`deploy/tls/tolikopa.crt` → `/tls/tolikopa.crt`, read-only; `TLS_CERT_FILE`, defaulting to this
  path). Even if the API is compromised it cannot reach the private key. The API passes through
  only the `CERTIFICATE` blocks from the file.

## Panel response headers (Caddy)

`deploy/caddy/Caddyfile` sends on every response:

- **`Content-Security-Policy`** (enforced): `default-src 'self'; script-src 'self' 'sha256-…';
  style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:;
  connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none';
  form-action 'self'`. Derived from the build: the only inline script is the theme/language
  bootstrap in `web/index.html`, allowed **by its hash**; everything else is same-origin bundles,
  self-hosted fonts, same-origin `fetch` and the same-origin `wss://…/ws` socket (`connect-src
  'self'`). There is no `eval`/`new Function` in the bundles. `style-src 'unsafe-inline'` is
  needed for the inline styles React, motion and Recharts set; scripts stay hash-only.
  **Editing that inline script changes its hash**: put the new `sha256-…` into the Caddyfile.
  `web/Dockerfile` recomputes the hash of every inline script in `dist/index.html` and **fails the
  image build** if one is missing from the Caddyfile.
- **`Strict-Transport-Security: max-age=31536000`** — browsers honour it only over a connection
  whose certificate they trust (the GPO-distributed one) and never for an IP address; afterwards a
  certificate warning for the panel's name can no longer be clicked through.
- `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`;
  the `Server` header is removed.

## Build supply chain

The API image also builds `tolikopa-agent.exe`, which runs as SYSTEM on every PC, so the build is
pinned end to end:

- Base images by tag **and** digest (`golang`, `node`, `caddy`, `alpine` in the Dockerfiles; the
  TimescaleDB image in compose). Updating = replacing both (`docker buildx imagetools inspect`).
- Go: `go.mod`/`go.sum` are committed and complete; the build runs `go mod download` (verified
  against `go.sum` and the checksum database), `go mod verify` and `go build -mod=readonly`, with
  `GOTOOLCHAIN=local`. There is **no `go mod tidy` at build time** — it would resolve a new import
  to whatever version is latest at that moment. After changing imports, run `go mod tidy` locally
  and commit both files.
- npm: `npm ci` installs exactly `package-lock.json` (integrity hashes) and fails if it does not
  match `package.json`.
- Windows resources: `go run github.com/tc-hib/go-winres@<exact version>` turns the committed
  `winres/winres.json` of the agent and the setup tool into a `.syso` before `go build`. It is a
  build-time tool only (not a dependency of any shipped module), fetched by exact version through
  the module proxy and verified against the checksum database; its output is deterministic.

## The server's own Linux agent

`setup.sh` installs it as the systemd unit `tolikopa-agent`, running as root (it reads every
process's `/proc` entry and `statfs()`s every mount) but sandboxed: `ProtectSystem=strict`,
`ProtectHome=read-only`, `PrivateTmp=yes`, `NoNewPrivileges=yes`, and the only writable path
`ReadWritePaths=/var/lib/tolikopa` (state, spool, logs). `/etc/tolikopa/config.json` (600) is read
only. The same data is reported as without the sandbox (verified: mounts, processes, logged-on
user from `/run/systemd/sessions`).

## TLS

On first install `setup.sh` generates a **self-signed server certificate** (`deploy/tls/`):
EC P-256, 10 years, SAN = the panel's DNS name + the server's IP, `basicConstraints CA:FALSE`,
`extendedKeyUsage serverAuth`. Caddy serves this certificate (`tls /etc/caddy/tls/tolikopa.crt
/etc/caddy/tls/tolikopa.key`); Caddy's own internal CA is not used. The agent pins the certificate
in `config.json` → `tls_ca_pem` (Go accepts a self-signed leaf certificate in the pool) —
**`InsecureSkipVerify` is not used**.

**Why a leaf certificate rather than a CA.** This certificate is distributed by
`tolikopa-setup.exe` to every computer in the domain as a **trusted root** (so browsers open the
panel without warnings). A CA placed there would let anyone who compromised the key sign
certificates for **any website** — compromising the server would turn into a man-in-the-middle on
the whole domain's HTTPS traffic. A `CA:FALSE` leaf certificate, by contrast, is valid only for
itself: the only thing that can be impersonated with a stolen key is the panel's own name and IP.

**The setup tool refuses to distribute a CA.** `tolikopa-setup.exe` adds the certificate to the GPO
only if all of these conditions hold: a single certificate, self-signed, not a CA (no CA flag or
`keyCertSign`), with `serverAuth` usage, exactly one DNS name and no wildcard, at most one IP
address, bound to the bundle's `server_url` (the DNS name equals its host, or — for an IP
`server_url` — the IP address equals it), not expired. If they do not hold, it prints the reason
and does not distribute; so even a compromised server that puts a CA into `config.json` cannot
plant a trusted CA in the domain, nor a leaf for `*.corp.example.com` or someone else's site. The trust is part of the GPO: when the
certificate changes, the old thumbprints are removed from the GPO, and when the GPO is removed the
trust goes too.

**Certificate continuity.** Because the agents pin the certificate and the computers trust it,
`setup.sh` on an existing installation (`deploy/.env` present) does not generate a new certificate
if `deploy/tls` is missing; it stops. On a first run the certificate is created **before**
`deploy/.env` is written, so an interrupted first run never leaves that state behind; a
certificate already present on a first run is reused (never replaced) if it covers the names given,
otherwise the script stops. `deploy/tls/` must be backed up together with `deploy/.env`.

## Credential management

- The domain/SSH passwords you provide and the generated tokens are **not written to the repo**;
  they are used only in `.env` (gitignored) and during deployment. `.gitignore` covers the `*.env`,
  `config.json`, `*.pem` patterns.
- `setup.sh` generates the DB password, the ingest token and the JWT key **randomly**; it writes
  them to `.env` (mode `600`). The first administrator password does not go into `.env`.
- **Recommendation:** the shared administrator password used during installation is used for
  domain admin, SSH and (probably) elsewhere. This document previously **contained that password in
  plain text**; the value has been removed, but it must be considered exposed to anyone who has read
  the repo — rotating it is no longer a recommendation but a requirement. After installation:
  - Change the password of the Ubuntu `tolikopa` user; if possible, switch to SSH key-based login
    and disable password login.
  - Rotate the domain admin password and replace it with a separate, strong value.
  - If these passwords are reused across different systems, use a separate password for each.

## Agent binary signing

If possible, `tolikopa-agent.exe` should be signed with Authenticode — an unsigned exe that runs as
LocalSystem, reads the event log and talks to the network is a classic AV/SmartScreen false
positive. Without a signature, a Defender exclusion / ASR exemption via GPO may be needed
(GPO-DEPLOYMENT.md).

### Antivirus and SmartScreen: why the download is a zip

Earlier versions served `tolikopa-setup.exe` / `tolikopa-update.exe` with the installation's zip
bundle **appended** to the executable. Microsoft Defender quarantined that download as
`Trojan:Win32/Wacatac.B!ml` (a machine-learning verdict, a false positive), for two reasons in
that design:

1. **A PE with a zip overlay** is the classic dropper / self-extractor layout that ML models are
   trained to distrust.
2. **Every download had a unique hash** (manifest, `config.json` and the token differ per
   installation and per download), so the file could never accumulate SmartScreen/Defender
   reputation, and a false-positive submission to Microsoft would only ever have cleared that one
   download.

Now the download is `tolikopa-setup.zip` / `tolikopa-update.zip`: the exe **exactly as built**
(byte-identical across installations of a build), `tolikopa.bundle` (a plain zip — manifest + files,
validated exactly as before) and a `README.txt`. The tool reads the bundle only from its own
folder (`os.Executable` + `EvalSymlinks`), never from the working directory, so a file planted
where the admin happens to open a console is not picked up. A bundle appended to the exe is still
accepted for old downloads. In addition, the setup tool and the agent carry a Windows **version
resource** and an **application manifest** (`asInvoker`: the agent runs as a service, the setup
tool uses the admin's domain rights, not local elevation), generated at image build time with a
pinned `go-winres` (server/Dockerfile). Troubleshooting a quarantine: GPO-DEPLOYMENT.md,
"Microsoft Defender quarantines the downloaded tool".

## Hardening checklist

- [ ] `.env` file permissions `600`, owned by root (`setup.sh` creates it so).
- [ ] Caddy ingest allowlist active.
- [ ] (Optional) A custom image built with `caddy-ratelimit` and an ingest rate limit added.
- [ ] First administrator created via `setup.sh` (console) or the setup code (or the first admin password in `.env` changed).
- [ ] `deploy/tls/tolikopa.key` mode `600`; `deploy/.env` and `deploy/tls/` backed up.
- [ ] The agent SHA-256 printed in the setup tool's plan compared with the value in the panel (optional: `Get-FileHash` on the distributed file).
- [ ] Initial panel admin password changed.
- [ ] 2FA enabled on administrator accounts; recovery codes stored somewhere safe.
- [ ] (Recommended) "Require Two-Factor Authentication" turned on in the general settings.
- [ ] At least two active administrator accounts exist (so if one is locked out, the other can reset it).
- [ ] Ubuntu `ufw`: only 443 + SSH open — and remember ufw does not filter Docker-published ports: use `BIND_ADDR`, `DOCKER-USER` or the network firewall for 80/443.
- [ ] Panel answers with `Content-Security-Policy` and `Strict-Transport-Security` (`curl -sI --cacert deploy/tls/tolikopa.crt https://<name>/`).
- [ ] Ubuntu SSH password login disabled (key-based).
- [ ] DB port (5432) only on the compose internal network; not exposed to the host.
- [ ] Shared passwords rotated.
- [ ] Agent pins the panel certificate, TLS verification on.
- [ ] (Optional) `tolikopa-agent.exe` signed.
