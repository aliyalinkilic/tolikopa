# API Contract

All endpoints live under `/api`. TLS is terminated by Caddy. Two kinds of identity:

- **Agent** → `Authorization: Bearer <INGEST_TOKEN>` (only `/api/v1/ingest`).
- **Admin** → `tolikopa_session` httpOnly+Secure+SameSite=Lax cookie (JWT). Write requests
  (POST/PUT/PATCH/DELETE) additionally require the `X-CSRF-Token` header (the `csrf` value from the
  login response; it can also be read from the `tolikopa_csrf` cookie).

Time values are ISO-8601 UTC (`2026-07-03T09:00:00Z`). Byte values are `int64`.

## 0. Error format and error codes

**Every** error response (auth, all endpoints, ingest, the router's own 404/405/413, WebSocket
upgrade rejection) carries the same JSON body:

```jsonc
{ "code": "password_too_weak",                       // machine-readable, snake_case — the UI translates THIS
  "message": "password must be at least 10 characters",  // English fallback text (for logs / curl)
  "params": { "min_length": 10 } }                    // optional: values to be inserted into the translated text
```

The server does **not** produce user-facing language; the panel builds the Turkish/English text
itself from `code` (+ `params`). For an unknown `code`, `message` may be shown. In 5xx responses
`message` is always generic (driver/SQL text never reaches the client).

| `code` | HTTP | Meaning |
|--------|------|-------|
| `invalid_request` | 400 | malformed body/parameter (`params.field`, if present, is the offending field) |
| `invalid_credentials` | 401 | wrong username or password (login) |
| `account_disabled` | 401 | account disabled (at login only after a correct password; on every request for an open session) |
| `session_required` | 401 | no session cookie |
| `session_invalid` | 401 | session invalid / expired / revoked (epoch) / account deleted |
| `csrf_failed` | 403 | `X-CSRF-Token` missing or wrong |
| `admin_required` | 403 | `admin` role required |
| `forbidden` | 403 | generic 403 |
| `rate_limited` | 429 | too many failed attempts; `Retry-After` header + `params.retry_after` (s) |
| `mfa_required` | — | reserved code; when login needs 2FA it **does not return an error**, it returns `200 {mfa_required:true,…}` |
| `mfa_invalid` | 401 (login/mfa) · 400 (`/api/me/*`) | verification code / recovery code wrong or already used |
| `mfa_token_invalid` | 401 | `mfa_token` invalid / expired / the account changed in the meantime — restart login |
| `mfa_already_enabled` | 409 | 2FA already enabled |
| `mfa_not_enabled` | 409 | 2FA not enabled |
| `mfa_setup_required` | 409 | no pending setup — call `POST /api/me/2fa/setup` first |
| `mfa_enrollment_required` | 403 | the "Require 2FA" policy is on and this account has no 2FA (§2.4) |
| `mfa_required_by_policy` | 409 | you cannot turn off your own 2FA while "Require 2FA" is on |
| `enable_own_2fa_first` | 409 | to turn on "Require 2FA", first enable your own 2FA |
| `password_too_weak` | 400 | < 10 characters (`params.min_length`) or same as the current password (`params.reason="same_as_current"`) |
| `password_too_long` | 400 | > 72 bytes (bcrypt limit; `params.max_bytes`) |
| `password_incorrect` | 400 | current password wrong (while signed in; **not 401**, the session is valid) |
| `username_taken` | 409 | username in use (case-insensitive) |
| `user_not_found` | 404 | no such panel user |
| `last_admin` | 409 | at least one enabled admin must remain |
| `cannot_modify_self` | 409 | this change cannot be applied to your own account |
| `not_found` | 404 | resource does not exist (machine, rule, alert, unknown path) |
| `method_not_allowed` | 405 | — |
| `payload_too_large` | 413 | body / row-count limit exceeded |
| `unsupported_media_type` | 415 | — |
| `category_rule_conflict` | 409 | classification rule could not be added (same `match_type`+`match_value` exists) |
| `too_many_items` | 400 | limit exceeded in a bulk request (`params.max`) |
| `machine_not_archived` | 409 | purge: only archived machines can be deleted permanently (`params.machine_ids`, `params.count`); nothing was deleted |
| `dry_run_required` | 409 | hygiene: a dry-run result must be observed first (§10) |
| `unknown_catalog_item` | 400 | hygiene: item not in the catalog (`params.kind`, `params.item_id`) |
| `setup_already_done` | 409 | first-run setup: an admin account already exists (§11) |
| `setup_code_invalid` | 401 | first-run setup: wrong setup code (§11) |
| `setup_incomplete` | 409 | setup incomplete; `params.missing: [...]` (`server_url`, `root_ca`, `artifacts`, `domain` or an invalid field name) (§11) |
| `artifacts_unavailable` | 503 | build artifacts (agent / setup tool) are not present on the server (§11) |
| `ingest_token_required` / `ingest_token_invalid` | 401 | agent ingest identity |
| `server_busy` | 429 | ingest backpressure (`Retry-After`) |
| `server_error` | 5xx | server error |

**Request bodies are strict.** Every JSON write endpoint (except the agent ingest) decodes its
body strictly: an empty body, a key the endpoint does not declare (e.g. the fields wrapped in an
extra object), a value of the wrong JSON type, or trailing data after the document is
`400 invalid_request`, with `params.field` naming the offending key where there is one; a
non-JSON `Content-Type` is `415 unsupported_media_type`. A partial-update endpoint additionally
rejects a body that names none of its fields. Nothing is ever "accepted" while being ignored.

---

## 1. Agent ingest

### `POST /api/v1/ingest`
- Headers: `Authorization: Bearer <token>`, `Content-Type: application/json`,
  `Content-Encoding: gzip` (optional but recommended), `Idempotency-Key: <uuid>`.
- Body (one machine's batch):

```jsonc
{
  "batch_id": "6f9…",
  "machine": {
    "machine_id": "b1d…",          // the agent's persistent UUID
    "hostname": "ACCOUNTING-01",
    "os": "Windows 11 Pro", "os_build": "22631",
    "agent_version": "1.0.0", "domain": "CORP", "kind": "windows",
    "ip_addrs": ["10.0.0.57"],   // the machine's OWN IPv4 addresses (up, excluding loopback/APIPA, ≤8); refreshed on every batch
    "boot_time": "2026-07-03T06:00:00Z"
  },
  "samples": [{
    "ts": "2026-07-03T09:00:00Z",
    "cpu_pct": 12.5, "mem_used": 8000000000, "mem_total": 16000000000,
    "net_sent_bytes": 123456789, "net_recv_bytes": 987654321,   // cumulative counter
    "logon_user": "CORP\\ayse", "session_state": "active",   // active | idle | locked | disconnected | none
    "disks": [{ "mount": "C:", "used": 250000000000, "total": 500000000000 }],
    "processes": [{ "name": "chrome", "cpu_pct": 8.1, "mem_bytes": 900000000 }]
  }],
  "events": [{ "ts": "…", "kind": "app_crash", "process_name": "excel",
               "detail": { "exception": "0xc0000005" } }],
  "disk_health": [{ "disk": "C:", "smart_ok": true, "predict_fail": false }],
  "remediation": {                                  // optional, low cadence (§10)
    "catalog_version": "v1-14da70d24ee0",
    "policy_revision": "7",                         // the revision the agent has APPLIED
    "allowed": { "apps": true, "startup": true, "enforce": false },   // local GPO kill switch
    "present": [{ "kind": "app", "item_id": "weather", "present": true, "scope": "both" },
                { "kind": "startup", "item_id": "startup.spotify", "present": true, "enabled": true }],
    "attempts": [{ "ts": "…", "kind": "app", "item_id": "weather", "action": "remove",
                   "outcome": "ok", "duration_ms": 120 }],
    "checked_at": "2026-08-09T09:00:00Z"
  },
  "sent_at": "2026-07-03T09:01:00Z"
}
```

Further optional keys (all mirrored field-for-field in `agent/internal/model` and
`server/internal/model/ingest.go`; `server/internal/model/ingest_parity_test.go` fails on any
drift): `machine.cpu_model`, `machine.cpu_cores`;
`update_status: {pending_count, reboot_pending, last_search, last_install, checked_at}` (counts and
timestamps only, `-1` = unknown); `app_focus: [{name, seconds}]` (foreground application base name
only); `battery: {present, percent, ac_connected, charging, design_capacity, full_charge_capacity,
health_pct, cycle_count, checked_at}` (`-1` = unknown).

- **Validation:** `machine_id` is required; a `ts` later than `now()+5min` or earlier than
  `now()-7d` is rejected; `cpu_pct` is clamped to 0-100; size limits (≤240 samples, ≤20 disks,
  ≤5000 process rows, ≤512 remediation `present`, ≤512 remediation `attempts`, `item_id` ≤64,
  `error_code` ≤32 characters, ≤1MB body).
- **Server processing:** `machine`/`machine_live` upsert (automatic registration on first sight);
  network delta computation using `boot_time`; process name→`category_id`; `ON CONFLICT DO NOTHING`
  (idempotent).
- **Responses:** `202` `{ "accepted": N, "rejected": [{ "ts": "…", "reason": "…" }], "policy": {…} }` ·
  `401` invalid token · `413` too large · `429` `Retry-After` (overload). Error bodies use the §0
  format (`ingest_token_required`, `ingest_token_invalid`, `payload_too_large`,
  `invalid_request`, `server_busy`, `server_error`); the agent looks only at the HTTP status code.
  `rejected[].reason` is free text that goes to the agent log; it is not part of this contract.

**Revision handshake (`policy`):** the server resolves the policy that applies to the machine
(fleet-wide selection minus tag exemptions) and adds `policy` to the response **only** if the
resolved revision differs from the one the agent reported; if they are equal the field is not sent at
all, so the body stays small in the steady state.

```jsonc
{ "accepted": 4,
  "policy": { "revision": "7", "enabled": true, "dry_run": true,
              "apps": ["weather","news"], "startup": ["startup.spotify"] } }
```

`revision` is a **string**. Machines exempted by a tag receive a revision of the form
`"<revision>-x"` and `enabled:false` — since a tag change does not bump the fleet revision, without
the suffix the machine would match the revision it already applied and never see the change.

Backward compatibility: old agents that send **no** `remediation` block and an **empty**
`policy_revision` keep working (an empty revision never matches → the policy is sent, the old agent
ignores it). The `rejected` field is retained.

---

## 2. Identity (admin)

### 2.1 Session

| Endpoint | Body | Response |
|----|-------|-------|
| `POST /api/auth/login` | `{username,password}` | `200 {username,role,csrf,mfa_enrollment_required}` + Set-Cookie — **or**, if 2FA is enabled, `200 {mfa_required:true, mfa_token}` (**no** cookie) |
| `POST /api/auth/login/mfa` | `{mfa_token, code}` | same body as a successful login + Set-Cookie |
| `POST /api/auth/logout` | — | `204` (the token is revoked; if the revocation cannot be written, the cookie is still cleared and the response is `500`) |
| `GET  /api/auth/me` | — | `MeResponse` |

- `mfa_token` is opaque, valid for **5 minutes**, and only for this step: it is not accepted as a
  session cookie (separate `aud` + separate signing key). It becomes invalid if the account's
  password is reset, the account is disabled or 2FA is turned off → `mfa_token_invalid`.
- `code`: a 6-digit TOTP **or** a recovery code (`xxxxx-xxxxx`; dashes/spaces/uppercase don't
  matter). A recovery code is **single-use**. The same TOTP code is not accepted twice.
- **The second step is also counted per account** (independent of IP): after 5 wrong codes, that
  account's code step is locked from every address (`429 rate_limited`); code attempts on the
  `/api/me/2fa/*` endpoints write to the same counter. A password change/reset clears this lock.
- The username matches **case-insensitively** at login (`Ayse` = `ayse`).
- **Per-address limit:** the password step is additionally counted per client IP, independent of the
  username: after **20** wrong passwords from one address, that address is locked with the same
  increasing durations (against trying one password on many accounts). A correct password resets
  this counter.
- **Attempt limit:** login and login/mfa are counted in memory per (username, client IP). After 5
  failed attempts, lockout: 1 min → 5 min → 15 min → 1 h (increasing). While locked, even a correct
  password is rejected: `429 rate_limited` + `Retry-After`. The password step and the code step have
  separate counters.
- Error codes: `invalid_request`, `invalid_credentials`, `account_disabled`, `rate_limited`,
  `mfa_token_invalid`, `mfa_invalid`.

```jsonc
// GET /api/auth/me
{ "username": "jdoe", "role": "admin", "display_name": "Jane Doe",
  "totp_enabled": true,
  "prefs": { "theme": "dark", "lang": null },   // theme: dark|light|system|null (null = never chosen; an explicitly chosen "system" stays "system"); lang: tr|en|null (null = installation default)
  "created_at": "2026-07-01T08:00:00Z", "last_login_at": "2026-10-13T06:12:00Z",   // last_login_at may be null
  "mfa_enrollment_required": false }             // always present (§2.4)
```

**Session revocation.** The JWT is not trusted on its own: on every request a single row is read
from `admin_user`; if the account has been deleted/disabled or `session_epoch` differs from the one
in the token, the request gets `401`. The role always comes from the database (a role change takes
effect on the next request). Password change/reset, disabling, deleting and enabling 2FA drop **all**
of the user's sessions; open WebSocket connections are closed too. **Logout** revokes only that token
(`revoked_session`, token ID `jti`): a copy of the cookie no longer works either, and the user's
other sessions are unaffected.

### 2.2 My account — `/api/me` (everyone who is signed in; writes are CSRF-protected)

| Endpoint | Body | Response |
|----|-------|-------|
| `PUT  /api/me/profile` | `{display_name}` (≤80 characters; empty = clear) | `204` |
| `PUT  /api/me/prefs` | `{theme?, lang?}` (at least one; any other key → `invalid_request`) | `204` |
| `PUT  /api/me/password` | `{current_password, new_password}` | `204` + renewed cookie |
| `POST /api/me/2fa/setup` | `{password}` | `{secret, otpauth_url}` |
| `POST /api/me/2fa/enable` | `{code}` | `{recovery_codes: [10 × "xxxxx-xxxxx"]}` + renewed cookie |
| `POST /api/me/2fa/disable` | `{password, code}` | `204` |
| `POST /api/me/2fa/recovery-codes` | `{password, code}` | `{recovery_codes: [...]}` |

- **prefs:** a key that is not sent is **unchanged**; a key explicitly sent as `null` is **deleted**
  (`theme` → reads as `null`, `lang` → installation default). Invalid value → `invalid_request`
  (`params.field`).
- **password:** at least 10 characters, at most 72 bytes, different from the current password. **All
  other sessions** of the user become invalid; the calling session's cookie is reissued with the same
  CSRF value (the tab stays open). Errors: `password_incorrect` (400), `password_too_weak`,
  `password_too_long`, `rate_limited`.
- **2fa/setup:** requires the current password (so that a stolen cookie alone cannot bind someone
  else's authenticator to the account); if wrong, `password_incorrect` (400). It generates a
  **pending** secret and does **not** enable 2FA. Calling it again changes the pending secret.
  `secret` is base32 (unpadded, 32 characters);
  `otpauth_url = otpauth://totp/Tolikopa:<username>?secret=…&issuer=Tolikopa&algorithm=SHA1&digits=6&period=30`.
  If 2FA is already enabled, `mfa_already_enabled`.
- **2fa/enable:** verifies the code against the pending secret, enables 2FA and returns the recovery
  codes **once**. Other sessions are dropped (the caller's cookie is renewed). Errors: `mfa_invalid`
  (400), `mfa_setup_required`, `mfa_already_enabled`, `rate_limited`.
- **2fa/disable**, **2fa/recovery-codes:** require password + code (TOTP or recovery code). Once new
  recovery codes are generated, the old ones are invalid. Errors: `mfa_not_enabled`,
  `password_incorrect` (400), `mfa_invalid` (400), `rate_limited`; for disable also
  `mfa_required_by_policy`.
- Password/code attempts on `/api/me/*` are subject to the same increasing lockout per (user, IP).
- TOTP: RFC 6238, SHA-1, 6 digits, 30 s, ±1 step tolerance.

### 2.3 General settings

| Endpoint | Authorization | Body / response |
|----|-------|---------------|
| `GET /api/settings/public` | **none** (for the login page) | `{org_name, default_lang, version, require_2fa, needs_admin, setup_complete}` (`require_2fa` §2.4; the last two §11) |
| `GET /api/admin/settings/general` | admin | `{org_name, default_lang, require_2fa, work_hours}` |
| `PUT /api/admin/settings/general` | admin | same body → `204` (a field that is not sent is unchanged; at least one must be sent) |

`org_name` ≤120 characters (default `""`), `default_lang ∈ {tr,en}` (default `tr`),
`require_2fa` boolean (default `false`). `default_lang` is also the language of alert e-mails and of
the stored fallback alert text. `version` is the server build version (`"dev"` if
`-ldflags -X …/buildinfo.Version=` is not given).

**`work_hours`** (app_setting `work_hours`; the schedule for work-hours-only devices, §6.1):

```jsonc
{ "days": [1,2,3,4,5],          // ISO weekdays, 1 = Monday … 7 = Sunday
  "start": "09:00", "end": "17:30", // HH:MM wall clock, start inclusive, end exclusive, end > start
  "tags": [],                    // machine tags (machine.tags), matched case-insensitively, e.g. ["Office"]
  "grace_minutes": 60,           // 0–240: no NEW offline alert in the first/last N minutes of the day
  "offline_after_minutes": 60,   // 5–480: minimum offline time before a work-hours device alerts
  "stale_after_days": 1,         // 1–30: whole work days missed before a device counts as stale (§6.1)
  "timezone": "Europe/Istanbul" } // read-only; ignored on PUT
```

Shown values are the defaults used until an admin saves a schedule. With the default empty `tags`
no machine is a work-hours device: every machine is monitored 24/7 until tags are chosen. On `PUT`, `work_hours`
replaces the whole schedule: every field except `timezone` and `stale_after_days` is required
(an omitted `stale_after_days` keeps its current value; `400 invalid_request`,
`params.field = "work_hours.<field>"` on a missing or invalid one). Days/tags are de-duplicated
(tags case-insensitively, first spelling kept). No overnight schedules and no public holidays yet.

### 2.4 The "Require 2FA" policy (`require_2fa`)

Applies to all panel accounts (admin and viewer). It is **mandatory enrollment**, never a **denied
login** — it cannot lock anyone out:

- Turning it on: if the calling admin's own 2FA is not enabled, `409 enable_own_2fa_first`.
- A user without 2FA signs in **normally** with their password and gets a real session; but while
  `require_2fa=true` and `totp_enabled=false`:
  - the login success body and `GET /api/auth/me` → `"mfa_enrollment_required": true` (otherwise
    `false`; the field is always present);
  - **every** endpoint that requires identity returns `403 mfa_enrollment_required`; exceptions:
    `/api/auth/*`, `/api/me/2fa/setup`, `/api/me/2fa/enable`, `/api/me/prefs` (and
    `/api/settings/public`, which needs no identity anyway). The WebSocket upgrade is rejected too.
  - once `/api/me/2fa/enable` completes, the restriction is lifted **immediately**; no re-login is
    needed.
- While the policy is on, `POST /api/me/2fa/disable` → `409 mfa_required_by_policy`. An admin
  applying `reset-2fa` to a user keeps working (that user falls back into mandatory enrollment on
  their next request).
- The setting is cached in memory; `PUT` invalidates it immediately. A change made from the command
  line (docs/SECURITY.md — emergency commands) reaches the running server within 15 s at most.

All endpoints below require the session cookie. Those that require `role=admin` are under
`/api/admin/*`.

---

## 3. Fleet (`machine_live` only)

| Endpoint | Parameters | Response |
|----|--------------|-------|
| `GET /api/fleet` | `status=online\|offline`, `user=`, `q=` (hostname / machine id substring), `tag=` (machine group tag) | `FleetMachine[]` |
| `GET /api/fleet/summary` | — | `FleetSummary` (online/offline/active/**idle** counts, alert distribution keyed by `warning`/`critical`, `avg_cpu` 0-100, `avg_mem` **percent 0-100** — unlike `FleetOverview.avg_mem`, which is a 0-1 ratio; `agent_update {version,current,total}`: Windows PCs running the agent build this server ships, omitted without build artifacts, Linux not counted) |
| `GET /api/fleet/overview` | — | `FleetOverview` (home dashboard; see below) |

`FleetMachine`: `{machine_id,hostname,kind,online,cpu_pct,mem_ratio,disk_min_free_pct,logon_user,
session_state,open_alerts,last_seen,agent_version}`. `online = now()-last_seen < OFFLINE_WARN`.

**`session_state` values:** `active | idle | locked | disconnected | none`. `idle` = session open +
unlocked but no user input for ≥5 min. `idle` is never counted as "active" in any usage computation;
a backward-compatible `idle` count was added to `FleetSummary`
(`{total,online,offline,active,idle,alerts_by_severity,avg_cpu,avg_mem}`).

### `GET /api/fleet/overview`  (alias: `GET /api/v1/fleet/overview`)

The single-shot payload of the home dashboard. Live parts come **only from `machine_live`**;
`history` from `metric_5m` (real-time aggregation on); `critical_disk` from the latest
`disk_usage_sample` row per mount (6-hour window).

```jsonc
{
  "generated_at": "2026-07-24T09:00:00Z",
  "totals": { "total": 40, "online": 33, "offline": 7, "in_use": 21, "idle": 6, "locked": 4 },
  "health": { "score": 87, "issues": [ { "key": "offline", "count": 7, "penalty": 5 } ] },
  "avg_cpu": 23.4,                     // 0-100, online machines
  "avg_mem": 0.61,                     // ratio 0-1, online machines
  "history": [ { "ts": "…", "cpu_avg": 21.0, "mem_ratio_avg": 0.58 } ],            // last 24h, 5min buckets
  "top_cpu": [ { "machine_id": "…", "hostname": "…", "logon_user": "…", "cpu_pct": 97.1, "mem_ratio": 0.71 } ],   // limit 8
  "critical_mem": [ { "machine_id": "…", "hostname": "…", "logon_user": "…", "mem_ratio": 0.93, "mem_used": 15000000000, "mem_total": 16000000000 } ],  // >=0.90, limit 8
  "critical_disk": [ { "machine_id": "…", "hostname": "…", "mount": "C:", "used_pct": 94.2, "free_bytes": 29000000000 } ],  // >=90% full, limit 8
  "recent_events": [ { "ts": "…", "machine_id": "…", "hostname": "…", "kind": "app_crash", "process_name": "excel", "severity": "warning" } ],  // last 24h, limit 10
  "recent_alerts": [ /* AlertDTO[] — same shape as GET /api/alerts; status<>'resolved', limit 10 */ ]
}
```

Meanings: `totals` from `machine_live` (`online = now()-ts < OFFLINE_WARN`; `in_use` = online +
`session_state=active`; `idle` and `locked` likewise — Linux hosts count toward `total`/`online`
but never toward these three, since their "active" only means a logind/SSH session exists). `top_cpu`: online machines, current CPU
descending. `critical_mem`: online + `mem_ratio ≥ 0.90`, descending.

**Health score formula:** `score = max(0, 100 − Σ penalty)`. Only issues with `count>0` are
listed; the penalty for a present issue is at least 1. With `pay(n, base, weight) =
min(weight, round(weight × n / base))`:

| key | condition | penalty |
|-----|-------|---------|
| `offline` | offline machine | `pay(n, total, 30)` |
| `high_cpu` | online, `cpu_pct > 90` | `pay(n, online, 15)` |
| `high_mem` | online, `mem_ratio ≥ 0.90` | `pay(n, online, 15)` |
| `low_disk` | `disk_min_free_pct ≤ 10` (≥90% full) | `pay(n, total, 20)` |
| `smart` | machine with a SMART fail / predict-fail disk | `min(25, 10×n)` |
| `reboot_pending` | pending reboot | `min(10, 2×n)` |
| `crit_alerts` | open (`status<>'resolved'`) critical alert | `min(20, 5×n)` |

---

## 4. Machine detail

| Endpoint | Parameters |
|----|--------------|
| `GET /api/machines/{id}` | current snapshot + identity + disks + disk health |
| `GET /api/machines/{id}/metrics` | `from,to,res=raw\|5m\|1h,metrics=cpu,mem,net` → `MetricSeries` |
| `GET /api/machines/{id}/disks` | `from,to` → usage series per mount |
| `GET /api/machines/{id}/top-processes` | `from,to,by=cpu\|mem,limit` → `TopProcess[]` |
| `GET /api/machines/{id}/categories` | `from,to` → `CategoryShare[]` (proc_cat_1h) |
| `GET /api/machines/{id}/sessions` | `from,to` → `SessionItem[]` (usage_session) |
| `GET /api/machines/{id}/events` | `kind,from,to` → `EventDTO[]` |
| `GET /api/machines/{id}/alerts` | `status,from,to` → `AlertDTO[]` (newest first, ≤500) |
| `GET /api/machines/{id}/remediation` | — → `MachineRemediation` (§10) |
| `PUT /api/admin/machines/{id}/labels` | `{labels?: {room,owner,dept,…}, tags?: [string]}` → `200 {labels, tags}` (stored values) |

If `from`/`to` are not given, the series endpoints default to the last 6 hours. **Exception:**
`/alerts` treats `from`/`to` as optional filters on `opened_at` with **no** default window, so a
still-open alert raised days ago is listed.

`labels` and `tags` are independent: an omitted key keeps its stored value, at least one must be
sent. Tags are trimmed, de-duplicated, and limited to 64 characters each.

If `res` is not given, the server picks the source based on the range: ≤6h → `raw`, ≤7d → `5m`,
otherwise → `1h`.

---

## 5. Users

| Endpoint | Response |
|----|-------|
| `GET /api/users` | `tag=` (optional department filter) → `UserSummary[]` (distinct `logon_user`, `display_name`, `tags`, last seen, current machine, online; Linux hosts' local accounts excluded) |
| `GET /api/users/{user}/activity` | `from,to` → `{heatmap:[{weekday 0=Mon…6, hour, active_minutes}], daily:[{date, active_minutes, loggedon_minutes}], total_active_minutes, total_loggedon_minutes}` (Europe/Istanbul) |
| `GET /api/users/{user}/apps` | `from,to` → `[{name, seconds}]` foreground time per application base name, top 20 |
| `GET /api/users/{user}/timeline` | `from,to` → `SessionItem[]` (across machines) |
| `GET /api/users/{user}/logons` | `from,to` (default last 90d) → `UserLogon[]` `{machine_id, hostname, started_at, ended_at\|null, active_seconds}` — uninterrupted spans during which the user was a machine's console user, newest first. Derived from raw `metric_sample` as (machine, user) islands (5 min of silence = new record); archived machines are included |
| `GET /api/users/{user}/network` | `from,to,res=1h` → `NetPoint[]` (net_user_1h) |
| `GET /api/users/{user}/categories` | `from,to` → `CategoryShare[]` (proc_cat_user_1h) |

`{user}` is URL-encoded (`CORP\ayse` → `CORP%5Cayse`).

| Endpoint | Description |
|----|----------|
| `PUT /api/admin/users/{user}` | admin: `{display_name, tags}` — **both required** (full replace) → `200 {logon_user, display_name, tags}` |
| `GET /api/admin/tags` | admin: `[{tag, user_count}]` — department tags in use |
| `GET /api/tags/{tag}/summary` | `from,to` → `{tag, users:[{logon_user, display_name?, active_minutes, top_category?}], category_totals: CategoryShare[]}` |

---

## 6. Alerts

| Endpoint | Description |
|----|----------|
| `GET  /api/alerts` | `status,severity,machine,rule,tag,from,to` → `AlertDTO[]` (`tag` = machine tag, case-insensitive; newest 1000) |
| `POST /api/alerts/{id}/ack` | acknowledge the alert (`ack`) → `204`; `404 not_found` if there is no open alert |

### `AlertDTO` and multilingual text

```jsonc
{ "id": 123, "machine_id": "…", "hostname": "ACCOUNTING-01",
  "rule": "disk_low", "severity": "critical", "status": "open", "value": 4.2,
  "params": { "hostname": "ACCOUNTING-01", "mount": "C:", "free_pct": 4.2, "threshold": 5, "unit": "%" },
  "message": "ACCOUNTING-01 is low on free space on C:: 4.2%",   // FALLBACK text
  "opened_at": "…", "last_seen_at": "…", "resolved_at": null, "acked_at": null,
  "machine_tags": ["Office"],    // the machine's tags at read time (always an array)
  "work_hours": true,            // GET /api/alerts only: machine is a work-hours-only device (§6.1)
  "detail": { /* same object as params — for old clients */ } }
```

The client builds the text in its own language from **`rule` + `params`**. `message` is the
**fallback** text generated on the server in the `default_lang` in effect when the alert was last
evaluated (for old rows and unknown rules). `params` is always an object (never `null`); REST and
WebSocket (`alert.opened`) carry the same shape. `severity` (`warning|critical`) and `value` are
top-level fields.

| `rule` | `params` | TR template | EN template |
|--------|----------|-----------|-----------|
| `offline` | `hostname`, `age_seconds` (int, since last seen), `minutes` (int = age/60), `threshold` (s; absent for `kind:"stale"`), `unit:"s"`, `work_hours` (true for work-hours devices), `kind` (work-hours devices only: `not_started` \| `went_offline` \| `stale`, §6.1); for `stale` also `days` (int = age/86400), `stale_after_days`, `last_seen` (RFC 3339) | no kind: `{hostname} çevrimdışı ({minutes} dk görülmedi)`; `not_started`: `{hostname} bugün hiç açılmadı`; `went_offline`: `{hostname} gün içinde çevrimdışı oldu ({minutes} dk görülmedi)`; `stale`: `{hostname} uzun süredir sessiz ({days} gündür görülmedi) — kullanılmıyorsa arşivleyin` | `{hostname} is offline (not seen for {minutes} min)`; `{hostname} has not been switched on today`; `{hostname} went offline during the day (not seen for {minutes} min)`; `{hostname} has been silent for a long time (not seen for {days} days) — archive it if it is no longer in use` |
| `cpu_high` | `hostname`, `value` (percent 0-100), `threshold`, `unit:"%"`, `window_seconds` | `{hostname} CPU yüksek: %{value}` | `{hostname} CPU usage is high: {value}%` |
| `mem_high` | `hostname`, `value` (percent 0-100), `threshold`, `unit:"%"`, `window_seconds` | `{hostname} bellek yüksek: %{value}` | `{hostname} memory usage is high: {value}%` |
| `disk_low` | `hostname`, `mount`, `free_pct` (percent), `threshold`, `unit:"%"` | `{hostname} {mount} diskinde boş alan az: %{free_pct}` | `{hostname} is low on free space on {mount}: {free_pct}%` |
| `smart_fail` | `hostname`, `disk`, `smart_ok` (bool\|null), `predict_fail` (bool\|null) | `{hostname} {disk} diski SMART arızası bildiriyor` | `{hostname} disk {disk} reports a SMART failure` |
| `crash_storm` | `hostname`, `count` (int), `window_seconds`, `window_minutes` (int), `threshold`, `app` (most frequent faulting process base name), `apps` (distinct process count) | `{hostname} son {window_minutes} dk'da {count} çökme (en sık: {app})` | `{hostname} had {count} crashes in the last {window_minutes} min (mostly {app})` |

When the engine closes an alert itself for a reason other than "condition cleared", it adds
`params.resolved_reason`: `work_hours_end` (the work day ended) or `not_stale` (a stale alert
whose device reported again outside work hours), §6.1.

`threshold` is the threshold that fired (the warning or critical threshold, depending on
`severity`). **Rows written before this version have no `threshold` (and, for cpu/mem, no
`window_seconds`)**; the other derivable values (`hostname`, `minutes`, `window_minutes`, `unit`, …)
are filled in at read time — the client must tolerate a missing `threshold`. An open alert's `params`
is updated on every evaluation (30 s).

**Severity** is `info`, `warning` or `critical`. Only a newly opened **critical** alert sends an
e-mail; `info` and `warning` never do.

**E-mail:** critical alert e-mails are generated in the `default_lang` language (tr/en) from the
small message table on the server (`server/internal/alerts/i18n.go`); both languages are complete.

### 6.1 Rule evaluation (every 30 s)

| Rule | Condition | Opens | Clears |
|------|-----------|-------|--------|
| `offline` | seconds since `machine_live.ts` ≥ warn/crit | immediately | condition clear for 10 min |
| `cpu_high`, `mem_high` | `metric_5m` average over `for_seconds` ≥ warn/crit | immediately | value < threshold − 5 points (hysteresis), then 10 min |
| `disk_low` | latest free % per mount ≤ warn/crit | immediately | free > threshold + 2 points, then 10 min; **held** (neither refreshed nor resolved) while the machine sends no disk samples (off/asleep) |
| `smart_fail` | `disk_health` reports `predict_fail` or `smart_ok=false` | immediately (critical) | 10 min after the disk reports healthy |
| `crash_storm` | `app_crash`+`app_hang` events in `params.window_seconds` (default 600) ≥ warn/crit; processes in `params.ignore_apps` (base names, case-insensitive, `.exe` optional) never count | immediately | condition clear for **60 min** (a crash-looping service stays one alert) |

Archived machines are never evaluated. Default thresholds (`alert_rule`, tunable under
Settings → Alert Thresholds): offline 300/1800 s, cpu 85/95 % over 600 s, mem 95/98 % over 600 s
(0019; was 90/97), disk 15/5 % free, crash storm 3/6 per 10 min.

**Work-hours devices.** A machine carrying one of `work_hours.tags` (§2.3) is a work-hours-only
device (staff PC); every other machine keeps the 24/7 behaviour above. For a work-hours device the
`offline` rule is evaluated against the schedule, in Europe/Istanbul wall-clock time:

- Outside the schedule (non-working day, before `start`, from `end` on) no offline alert exists:
  an open one is resolved at once with `params.resolved_reason = "work_hours_end"` — a PC shut
  down at 17:35 never alerts, one left open at 17:30 does not nag overnight.
- Inside the schedule, offline time counts from `max(last_seen, today's start)`; a PC switched off
  yesterday evening is "offline for 0 min" at 09:00.
- The threshold is `max(rule warn, offline_after_minutes)` (a PC napping over lunch is not an
  incident). Severity is never critical (no e-mail for an absent user) and depends on the kind:
  a device **not seen at all since today's `start`** opens with severity **info** and
  `params.kind = "not_started"` ("not switched on today"); one seen today that then went offline
  is **warning** with `params.kind = "went_offline"`. An open `not_started` alert whose device
  briefly reports and goes offline again is updated in place to `went_offline`/warning.
- A **new** alert opens only after `start + grace_minutes` and before `end − grace_minutes`
  (morning boot-up, early leavers); an alert already open is kept until the day ends. With the
  defaults (09:00–17:30, grace 60, offline_after 60) new alerts open between 10:00 and 16:30; a
  PC still off at 10:00 on a workday alerts at 10:00.
- **Stale devices.** A device that has missed `stale_after_days` (default 1) **whole work days** —
  work windows that started after its `last_seen` and have ended — is *stale*. Instead of a new
  offline alert every workday it keeps (or, if none is open, opens once) a single `offline` alert
  with severity **info** and `params.kind = "stale"`, under the same dedup key `offline:<machine>`
  (an open `not_started` alert turns into it in place when the missed day ends). The stale alert is
  evaluated around the clock — it is **not** resolved at the end of work hours, at night or at the
  weekend — and resolves only when the device reports again (outside work hours at once with
  `resolved_reason = "not_stale"`, inside them through the usual 10-minute grace) or when the
  machine is archived. Non-working days do not count as missed days, so a PC switched off for the
  weekend is not stale on Monday morning. The wording suggests archiving the device if it is no
  longer in use.

---

## 7. Administration (admin)

| Endpoint | Description |
|----|----------|
| `GET/POST /api/admin/categories`, `PUT/DELETE /api/admin/categories/{id}` | program-type classification rules (CRUD); GET takes `window?` (default `7d`) for the match stats |
| `GET  /api/admin/categories/test` | `name` → `{name, category?, matches:[{id, category, match_type, match_value, priority}]}` — which rule wins now (first) and which it shadows |
| `GET  /api/admin/unclassified` | `from,to,sort?` (`machines`\|`count`\|`cpu`, default `machines`) → `[{name, count, machines, cpu_avg, cpu_share, last_seen}]`, at most 200; window clamped to the last 30 days, machine counts taken over the top 1000 names by count (by CPU for `sort=cpu`) |
| `POST /api/admin/reclassify` | `{window?, names?}` — reclassify the raw window (or only `names`) + refresh rollups |
| `GET/PUT /api/admin/alert-rules` | read/update alert thresholds (and the `crash_storm` ignore list) |
| `GET  /api/admin/crash-apps` | `window?` (default `7d`, at most `30d`), `limit?` (default 20, 1–50) → `{window_seconds, apps:[{app, crashes, machines, ignored}]}`: faulting process base names of `app_crash`/`app_hang` events of non-archived machines in the window, most crashes first; `ignored` = already in `crash_storm` `ignore_apps`. Suggestions for the ignore-list editor |

- **Category rule** body (POST, and PUT with the id in the **path** — the full rule as GET returns
  it): `{category, match_type, match_value, priority, enabled}` (`id` may be repeated in the PUT
  body but must equal the path). `category` 1-64 characters, `match_value` 1-128 (lower-cased),
  `match_type ∈ {exact, prefix, glob}` (empty → `exact`); otherwise `invalid_request` +
  `params.field`. A duplicate `match_type`+`match_value` → `409 category_rule_conflict` (create and
  update). Unknown id → `404`. DELETE → `204`. GET also returns read-only `builtin` (seed rule
  never edited; any PUT clears it), `samples`, `machines`, `last_seen` (how often the rule was the
  winning match over the window, from `proc_cat_1h`; `0` = never matched); create/update accept and
  ignore them.
- **unclassified** reads `proc_name_1h` and resolves each name against the **current** rules, so a
  name drops off the list as soon as a rule covers it (before any reclassify). `cpu_share` is the
  name's share (0..1) of all process CPU in the window; `machines` is a distinct count from the raw
  rows. Unknown `sort` → `invalid_request` (`params.field="sort"`).
- **categories/test** `name` 1-128 characters, else `invalid_request` (`params.field="name"`).
  `name` in the answer is the normalised form the rules see.
- **reclassify** `window`: `6h`, `24h`, `7d` … (default `24h`, at most `30d`); anything else →
  `invalid_request` (`params.field="window"`). `names` (optional, 1-200 names, each ≤128,
  normalised server-side) limits the pass to those process names, else `invalid_request`
  (`params.field="names"`). Response `{updated_rows, names, from, to}`.
- **alert-rules** PUT body is a bare array `[{rule, enabled, warn_threshold|null,
  crit_threshold|null, for_seconds}]`; the response is the full list as GET returns it. An unknown
  `rule` rejects the whole save (`invalid_request`, `params.rule`) and nothing is written.
  `crash_storm` also carries `ignore_apps: [string]` (GET always returns it for that rule, possibly
  `[]`; omitted on PUT = keep the stored list, `[]` = clear it). Entries are process **base names**:
  trimmed, lower-cased, a trailing `.exe` dropped (the form the engine matches), empties and
  duplicates removed; an entry with `/` or `\`, a control character or more than 128 bytes →
  `invalid_request` (`params.field="ignore_apps"`), more than 100 entries → `too_many_items`
  (`params.max`); `ignore_apps` on any other rule → `invalid_request`.
| `GET/POST /api/admin/panel-users` … | panel user management (§7.1) |
| `GET/POST /api/admin/users` | **legacy alias** for `panel-users` list/create (same handler). `PUT /api/admin/users/{user}`, however, is something else: the profile of a monitored **domain** user |
| `GET/PUT /api/admin/settings/general` | general settings (§2.3) |
| `GET  /api/admin/ingest-token` | `{masked, server_host, hint_code:"ingest_token_config", hint}` — `hint` is English fallback text, the UI translates `hint_code` |
| `POST /api/admin/machines/archive` | `{machine_ids:[…]}` (≤500) → decommission (archive) machines; resolve open alerts, close open sessions, audit record → `{changed}` |
| `POST /api/admin/machines/restore` | `{machine_ids:[…]}` → restore from archive → `{changed}` |
| `POST /api/admin/machines/purge` | `{machine_ids:[…]}` (≤50; more → 400 `too_many_items`, `params.max`) → **permanently delete** archived machines and their data (hygiene attempts are kept as audit trail, §10); any non-archived id → 409 `machine_not_archived`, nothing deleted; unknown ids ignored → `{purged}` |
| `GET  /api/admin/machines/archived` | archived machines (`archived_at`, `archived_by`, last seen) |
| `GET  /api/admin/remediation` | endpoint hygiene settings payload (§10) |
| `PUT  /api/admin/remediation/policy` | write policy + selection, bump revision, audit record (§10) |
| `GET  /api/admin/remediation/attempts` | `limit,outcome,item_id` → audit feed (§10) |
| `GET  /api/admin/remediation/failures` | open failures: last attempt failed + item still present, grouped by (item, outcome, error code); count of pairs that later recovered (§10) |
| `GET  /api/admin/remediation/impact` | `days` (1–14, default 7) → active-hour CPU/RAM averages before/after successful app removal, control group, per-app process footprint (§10) |
| `GET  /api/admin/remediation/posture` | Optimization page roll-up: per-platform switch counts, per-item progress, per-machine hygiene posture, Linux advisory recommendations (§10, read-only) |

### 7.1 Panel users (`/api/admin/panel-users`, admin only)

Accounts that can sign in to the panel (`admin_user`). `PanelUser`:
`{id, username, display_name, role: "admin"|"viewer", disabled, totp_enabled, created_at, last_login_at|null}`.

| Endpoint | Body | Response |
|----|-------|-------|
| `GET    /api/admin/panel-users` | — | `PanelUser[]` |
| `POST   /api/admin/panel-users` | `{username, password, role, display_name?}` | `201 PanelUser` |
| `PATCH  /api/admin/panel-users/{username}` | `{role?, disabled?, display_name?}` (at least one) | `200 PanelUser` (updated state) |
| `POST   /api/admin/panel-users/{username}/reset-password` | `{new_password}` | `204`; all of the target's sessions are dropped |
| `POST   /api/admin/panel-users/{username}/reset-2fa` | — | `204`; 2FA + recovery codes are deleted |
| `DELETE /api/admin/panel-users/{username}` | — | `204` |

- `username`: 1-64 characters; letters, digits and `. _ - @`. A second account differing only in
  case cannot be created (`username_taken`). If `role` is not given, `viewer`. The password rule is
  the same as §2.2 (`password_too_weak` / `password_too_long`) — the legacy `POST /api/admin/users`
  alias now enforces it too.
- `{username}` is sent URL-encoded. Unknown user → `404 user_not_found`.
- **Safeguards:**
  - `last_admin` (409): if no **enabled** (not disabled) admin would remain after the operation.
  - `cannot_modify_self` (409): an admin **cannot delete, disable or demote** their own account;
    nor can they reset their own password/2FA through these endpoints (that is done via
    `PUT /api/me/password` and `POST /api/me/2fa/disable`, which require re-authentication).
    They can change their own `display_name` via PATCH.
- Disabling, deleting and password reset drop the target's sessions immediately. A role change
  does not drop the session but takes effect on the next request.

---

## 8. WebSocket

### `GET /ws`  (cookie auth + origin check)
The upgrade goes through the same session check as REST (epoch, disabled account, "Require 2FA");
if rejected, the JSON error body from §0 is returned (`401 session_required|session_invalid|account_disabled`,
`403 mfa_enrollment_required`). Open connections of a user whose session is revoked are closed by
the server (close code 1008). The connection is revalidated once more after registration and then
**every 60 seconds** (token expiry + user row); on failure it is closed with 1008 — this is how an
expired token and a revocation made from another process (emergency commands) are caught (with at
most ~60 s delay). On 1008 the client should try to reconnect; if the upgrade returns `401`/`403`, it
should go to the sign-in / 2FA enrollment screen.

The client sends a channel subscription as JSON: `{ "subscribe": ["fleet","alerts","machine:<id>"] }`.
The server sends coalesced snapshots on a **2-5 s tick** (not on every ingest):

```jsonc
{ "type": "fleet.update",   "payload": { "machines": [ /* FleetMachine[] that changed */ ] } }
{ "type": "machine.metric", "payload": { "machine_id":"…","t":"…","cpu_pct":…,"mem_ratio":… } }
{ "type": "alert.opened",   "payload": { /* AlertDTO */ } }
{ "type": "alert.resolved", "payload": { "id": 123 } }
```

Channels: `fleet` → `fleet.update`; `alerts` → `alert.opened` / `alert.resolved`;
`machine:<id>` → `machine.metric` for that machine, sent on the same tick whenever its
`machine_live` row changed (`t` = the row's timestamp, `mem_ratio` 0-1). A subscribe message
replaces the client's whole subscription set.

---

## 9. Statistics (dashboard)

### `GET /api/stats/hourly-activity?days=7`  (alias: `GET /api/v1/stats/hourly-activity`)

`days`: 1-30 (default 7; out-of-range values are clamped to the bound). Fleet-wide "busy hours"
heatmap — from the `session_user_1h` cagg (including empty `logon_user`, machine-level activity;
Linux hosts excluded).
`dow` is ISO (1=Monday … 7=Sunday), `hour` 0-23, both in **Europe/Istanbul** local time.
`active_seconds = active_samples × 15` (nominal vitals cadence is 15 s — see docs/DATABASE.md);
`samples` = total vitals samples in the cell.

```jsonc
{ "days": 7, "cells": [ { "dow": 1, "hour": 9, "active_seconds": 84600, "samples": 7200 } ] }
```

### `GET /api/stats/top-apps?range=24h&by=cpu`  (alias: `GET /api/v1/stats/top-apps`)

`range ∈ {24h, 7d, 30d}` (default `24h`), `by ∈ {cpu, mem, focus}` (default `cpu`).
Fleet-wide application consumption — the `proc_name_1h` cagg + category resolution (the same
`process_category` cache as ingest; unmatched → `""`) + `focus_seconds` via LEFT JOIN from
`app_focus_user_1h`. Sorted descending by the `by` key, limit 15. `active_hours` = number of distinct
hour buckets in which the application appeared (float hours). `cpu_avg`/`mem_avg` are sample-weighted
averages; `machines` = peak distinct-machine count in any hour. PRIVACY: process base name only —
never a title/path/URL.

```jsonc
{ "apps": [ { "name": "chrome", "category": "browser", "cpu_avg": 12.3, "cpu_max": 96.0,
              "mem_avg": 850000000, "machines": 27, "focus_seconds": 44100, "active_hours": 21.0 } ] }
```

### `GET /api/stats/fleet-trend?range=24h`  (alias: `GET /api/v1/stats/fleet-trend`)

`range ∈ {24h, 7d}` (default `24h`; any other value → `400 invalid_request`, `params.field =
"range"`). Fleet health trend for the overview page — read from the `metric_5m` cagg (real-time
aggregation, so the live edge is included), re-bucketed with `time_bucket(…, 'Europe/Istanbul')`:
15-minute buckets for `24h`, 1-hour buckets for `7d` (`bucket_seconds`). The window starts at the
local bucket containing `now() − range`. Archived machines are excluded.

| Field | Meaning |
|---|---|
| `online` | distinct machines with at least one vitals sample in the bucket |
| `active` | distinct **non-Linux** machines whose last session state in at least one 5-minute sub-bucket was `active` (a Linux host's logind/SSH session is not a person at a desk — same rule as `/api/fleet/overview`) |
| `cpu_avg` | mean of the per-machine 5-minute CPU averages, percent 0-100 |
| `mem_ratio_avg` | mean of the per-machine 5-minute RAM ratios, 0-1 |

Buckets with no reporting machine are absent (not zero-filled). `summary.*.current` is the latest
bucket (may be partial), `avg` / `peak` are the mean / maximum over the returned buckets — for
`cpu_avg` / `mem_ratio_avg` that is the peak of the fleet average, not a single machine's maximum.

```jsonc
{
  "range": "24h", "bucket_seconds": 900, "generated_at": "2026-10-20T09:00:00Z",
  "points": [ { "ts": "2026-10-19T09:00:00Z", "online": 42, "active": 31,
                "cpu_avg": 18.4, "mem_ratio_avg": 0.62 } ],
  "summary": {
    "online":        { "current": 44,   "avg": 38.2, "peak": 47 },
    "active":        { "current": 33,   "avg": 21.5, "peak": 39 },
    "cpu_avg":       { "current": 21.0, "avg": 15.7, "peak": 34.9 },
    "mem_ratio_avg": { "current": 0.64, "avg": 0.58, "peak": 0.71 }
  }
}
```

### `GET /api/stats/slow-machines?days=7&limit=20&min_score=0`  (alias: `GET /api/v1/stats/slow-machines`)

`days`: 1-30 (default 7), `limit`: 1-200 (default 20), `min_score`: score threshold (default 0).
The "slow machines" diagnostic report. It uses no new agent capability — it is derived from
telemetry that is already collected. Linux hosts (`kind = 'linux'`, the server itself) are not
ranked: the score is tuned for user PCs, and a server's long uptime is not a slowness signal.

The score is **additive** (unlike the fleet health score, it is not subtracted from 100), clamped to
100, and every contributing factor is returned in `reasons[]` with its points, so that the panel can
explain the ranking instead of asserting it. A factor whose measurement has never been reported adds
**no** points — an unmeasured machine must not look healthy.

| `reasons[].key` | Weight | Source | `value` unit |
|---|---|---|---|
| `mem_pressure` | 30 | `metric_1h.mem_ratio_avg`, linear between 70%→95%; requires at least 3 hours of history | ratio 0-1 |
| `smart` | 25 | `disk_health.smart_ok = false OR predict_fail` | 1 |
| `low_ram` | 20 | `machine_live.mem_total` (<4.5 GB full · <8.5 GB 60% · <12 GB 25%) | GB |
| `disk_full` | 20 | fullest mount, linear between 85%→98% | percent 0-100 |
| `crashes` | 15 | `event.kind IN (app_crash, app_hang)` within the window, 3 points per crash | count |
| `weak_cpu` | 12 | `machine.cpu_cores` (≤2 full · ≤4 42%) | cores |
| `long_uptime` | 10 | `last_seen − boot_time` (≥30 d full · ≥14 d 60% · ≥7 d 30%) | days |

`severity`: `crit` ≥50, `warn` ≥25, otherwise `ok`. Sorted by score descending, ties by hostname.

Only machines seen within the window (`machine_live.ts >= now() - days`) are ranked; a machine that
has not reported for weeks has no history to assess. `uptime_days` is measured **against
`last_seen`, not `now()`** — otherwise a machine that is switched off would keep being scored as
"never rebooted".

Averages come from `metric_1h`; because this cagg is `materialized_only` with a 1-hour `end_offset`,
it can be up to ~2 hours stale (0003_caggs.sql). Negligible over a 7-day horizon; if a short-window
mode is wanted, `metric_5m` should be read (real-time aggregation was enabled in 0010). For disk, the
latest sample per mount within a 12-hour window is taken, then the fullest mount is chosen —
`machine_live.disk_min_free_pct` may be stale because it is preserved with COALESCE at ingest, and it
carries no mount name.

```jsonc
{
  "days": 7, "generated_at": "2026-08-09T12:00:00Z",
  "machines": [ {
    "machine_id": "…", "hostname": "CORP-LAPTOP-07", "logon_user": "CORP\\ayse",
    "online": true, "last_seen": "2026-08-09T11:59:12Z",
    "score": 62, "severity": "crit",
    "reasons": [ { "key": "mem_pressure", "points": 26, "value": 0.92 },
                 { "key": "low_ram", "points": 12, "value": 8.0 },
                 { "key": "disk_full", "points": 14, "value": 94.1 },
                 { "key": "long_uptime", "points": 10, "value": 41.3 } ],
    "mem_total": 8589934592, "mem_ratio_avg": 0.92, "mem_ratio_max": 0.98,
    "cpu_avg": 34.2, "cpu_cores": 4, "cpu_model": "Intel Core i5-8250U",
    "disk_mount": "C:", "disk_used_pct": 94.1, "disk_free_bytes": 14000000000,
    "uptime_days": 41.3, "crashes": 0, "smart_bad": false, "sample_hours": 168
  } ]
}
```

---

## 10. Endpoint hygiene (bloatware / sign-in startup)

### Authorization model — read this first

The agent carries a **compiled** allowlist and a **local GPO kill switch**. The server can only
**SELECT** from this vocabulary; the agent locally rejects an `item_id` it does not recognise and
reports it back as `outcome:"unknown_item"`. Extending the vocabulary requires a new
`tolikopa-agent.exe` (the audited GPO path), not a database write. These endpoints are a **UI mirror
and audit trail, never a command channel** (CLAUDE.md §Safety).

There are three mutually independent vetoes; the most restrictive one wins:
1. `policy.enabled=false` → the agent reconciles nothing (default).
2. `policy.dry_run=true` → evaluates and reports, changes nothing (default).
3. The GPO switches on the machine (`allowed.apps/startup/enforce`) → a local veto the panel cannot
   reach.

`machine_remediation_state` holds the presence of **allowlisted** items **only**; **it is not an
installed-software inventory**. Software the user installed themselves is never inspected,
transmitted or stored.

### `GET /api/admin/remediation`

Single-shot settings payload: the catalog (grouped by kind), the current policy, the fleet presence
count per item and a version-drift summary.

```jsonc
{
  "catalog_version": "v1-14da70d24ee0",       // the SERVER's embedded version
  "apps": [ {
    "kind": "app", "item_id": "weather", "family": "Microsoft.BingWeather_8wekyb3d8bbwe",
    "display_name": "Weather (MSN)", "category": "medya", "risk_tier": "safe",
    "scope": "both", "safe_default": true, "process_names": ["msnweather"],
    "notes": "…", "deprecated": false, "selected": true, "present_count": 27, "reported_count": 33
  } ],
  "startup": [ { "kind": "startup", "item_id": "startup.spotify", "family": "Spotify",
                 "display_name": "Spotify — at sign-in", "risk_tier": "safe",
                 "safe_default": true, "deprecated": false, "selected": false,
                 "present_count": 4, "reported_count": 33 } ],
  "policy": { "enabled": false, "dry_run": true, "excluded_tags": ["server"],
              "apps": ["weather"], "startup": [], "revision": 7,
              "updated_at": "…", "updated_by": "admin" },
  "fleet": { "machines": 40, "reporting": 33, "never_reported": 7,
             "catalog_stale": 2, "policy_current": 30, "kill_switch_off": 5 }
}
```

`deprecated:true` = an item removed from the vocabulary; it is **flagged, not filtered** (it may
still be selected or still be present on a machine). `present_count` comes from
`machine_remediation_state` (`present = true`, non-archived machines); it is **`null`** while no
machine has sent a hygiene report at all (unknown — not "nowhere"). `reported_count` = machines that
have sent a hygiene report (`fleet.reporting`): agents scan their whole compiled catalog and report
only what is present, so every reporting machine is the denominator for every item. `catalog_stale` = number of machines whose
`catalog_version` differs from the server's (the agent binary is old). `kill_switch_off` = machines
whose local GPO switch forbids the action. `policy.revision` is a **number** (whereas
`policy.revision` in the ingest response is a string).

Since migration `0020` the fleet block also carries the three switches separately:
`apps_not_allowed`, `startup_not_allowed`, `enforce_off` (reporting machines whose switch is known
to be off) and `switches_unknown` (reporting machines that have not reported since the server began
storing them). Startup items carry `enabled_count`: machines where the entry is present **and**
still runs at sign-in — a disabled entry stays present (its Run value is never deleted), so
`present_count` alone over-counts what is left to do.

### `PUT /api/admin/remediation/policy`

The body is the full desired state (the selection is replaced wholesale), so all five keys are
**required** — a missing one is `400 invalid_request` with `params.field` (a missing `apps` would
otherwise empty the selection, a missing `dry_run` would switch dry-run off). `force` is optional:

```jsonc
{ "enabled": true, "dry_run": true, "excluded_tags": ["server"],
  "apps": ["weather","news"], "startup": ["startup.spotify"] }
```

In a single transaction: the policy is written, `revision` is **incremented by 1**, the selection
table is refreshed, and a `before`/`after` record is written to `remediation_policy_audit` with the
JWT `Subject` + IP; then the ingest cache is refreshed. The response is `200` + the current `policy`
object (`RemediationPolicyDTO`). An `item_id` **not** in the catalog → `400 unknown_catalog_item`
(`params: {kind, item_id}`). Switching to enforcement without a dry-run result ever having been
observed → `409 dry_run_required` (overridden with `force:true`).

### `GET /api/admin/remediation/attempts?limit=100&outcome=&item_id=`

Audit feed. `limit` 1-500 (default 100), window the last 90 days. If `outcome` and `item_id` are
left empty, no filter is applied.

```jsonc
{ "attempts": [ { "ts": "…", "machine_id": "…", "hostname": "ACCOUNTING-01",
                  "kind": "app", "item_id": "weather", "display_name": "Weather (MSN)",
                  "action": "remove", "outcome": "ok", "duration_ms": 120 } ] }
```

Attempts outlive a permanent machine deletion (purge, §7). For a machine that no longer exists the
row carries `"machine_deleted": true` and `hostname` comes from its `purge` audit row (absent if
there is none); the panel shows it as plain text instead of a link to the machine page.

`action ∈ {remove, deprovision, disable, enable}`,
`outcome ∈ {ok, failed, dry_run, not_present, not_allowed, blocked, timeout, unknown_item}`.
`error_code` is only a **code** (`0x80073cfa`) — never a message or a path.
An empty `display_name` means the item has been removed from the catalog.

### Machine archive (decommissioning)

Archiving is **not deletion**: `machine.archived_at` is set and the machine disappears from all live
views (`/api/fleet`, `/fleet/summary`, `/fleet/overview`, alerts and the alert engine, the WebSocket
fleet stream, slow machines, the user list, hygiene posture, open failures and the control group).
Historical data ages out with the normal retention period; per-user totals keep counting it.
`GET /api/machines/{id}` also returns an archived machine (`archived_at`, `archived_by`). If ingest
arrives from an archived machine, the server **restores the machine automatically** and writes
`auto_restore` to `machine_archive_audit`. A repeated request for a machine already in that state is a
no-op and is not written to the audit.

**Permanent deletion (purge)** is the irreversible step after archiving and is only accepted for
machines that are archived at the time of the request (rows are locked `FOR UPDATE`, so a concurrent
restore or auto-restore cannot race it). One transaction writes a `purge` row to
`machine_archive_audit` (who: panel username, when, client IP, hostname, and how many hygiene
attempts were kept) and deletes the machine's rows from `metric_sample`, `disk_usage_sample`,
`process_sample`, `app_focus_sample`, `event`, `usage_session`, `alert`, `machine_live`,
`disk_health`, `machine_remediation_state` and finally `machine`. **`remediation_attempt` is kept**:
it is the actuation audit trail and ages out with its 365-day retention. In
`GET /api/admin/remediation/attempts` such rows carry `machine_deleted: true` and the hostname from
the purge audit row; the posture, open-failure and impact views leave them out. After commit the server refreshes the continuous
aggregates built on those hypertables from the machine's oldest raw row onwards (in the background,
best-effort; one refresh runs at a time and purges that commit meanwhile are merged into the next
pass), so per-machine and per-user rollups drop its rows too. If the agent on that PC is still
installed, its next report registers the machine again under the same `machine_id`, as a new active
machine with no history.

### `GET /api/admin/remediation/failures`

Each (machine, item) pair is classified by its **latest** action attempt in the last 90 days
(`dry_run` is ignored). A pair is *open* if the latest attempt is `failed|timeout|blocked|unknown_item`
**and** the machine has not reported the item as absent since that attempt. Response: `{open: [{kind,
item_id, display_name, outcome, error_code, machines, hostnames[≤8], stale_agents, last_ts}],
open_machine_items, resolved_machine_items, latest_agent_version}`. `latest_agent_version` is the
highest agent version reported by active machines; `stale_agents` counts how many of the machines in
the group are behind it. `/attempts` rows carry `resolved` by the same logic (on failure rows only).

### `GET /api/admin/remediation/impact?days=7`

For each machine, the `days` days before the first successful app removal are compared with the
`days` days after the last successful removal; only **active session** hours
(`session_user_1h.active_samples>0`) are counted, via `metric_1h`. `fleet` is the **paired** average
of machines with ≥ `min_hours` active hours on both sides; `control` is the Windows machines from
which nothing was removed, over the same calendar windows (relative to the median removal time) — net
effect = diff(fleet) − diff(control). `items[]` is the prior CPU/memory average of the processes in
the removed app's catalog `process_names`; since process sampling is top-N, it is a **lower bound**.
Only permitted aggregate metrics are read.

### `GET /api/admin/remediation/posture`

Read-only roll-up for the panel's Optimization page; nothing here selects or triggers an action.

```jsonc
{
  "generated_at": "…", "catalog_version": "v1-14da70d24ee0", "policy_revision": 7,
  "platforms": [ { "kind": "windows", "machines": 40, "reporting": 33, "allow_apps": 20,
                   "allow_startup": 18, "enforce": 15, "actuating": 15 },
                 { "kind": "linux", "machines": 1, "reporting": 0, "allow_apps": 0,
                   "allow_startup": 0, "enforce": 0, "actuating": 0 } ],
  "items": [ { "kind": "startup", "item_id": "startup.spotify", "display_name": "…",
               "selected": true, "pending": 4, "converged": 9, "dry_run": 0, "open_failures": 1 } ],
  "machines": [ { "machine_id": "…", "hostname": "PC-01", "tags": [], "excluded": false,
                  "reported_at": "…", "allow_apps": true, "allow_startup": false, "enforce": true,
                  "allowed": true, "catalog_stale": false, "policy_current": true,
                  "pending_apps": 2, "pending_startup": 0, "open_failures": 0,
                  "last_attempt_at": "…" } ],
  "linux": [ { "machine_id": "…", "hostname": "srv-01", "online": true, "last_seen": "…",
               "cpu_avg": 12.5, "mem_ratio_avg": 0.91, "mem_total": 8589934592,
               "disk_mount": "/", "disk_used_pct": 88.2, "disk_free_bytes": 5000000000,
               "uptime_days": 120.4, "smart_bad": false, "sample_hours": 168,
               "advice": [ { "key": "mem_pressure", "severity": "warn", "value": 0.91 } ] } ]
}
```

`items` lists selected items plus any item with an attempt in the 90-day window. `pending` =
targeted (not tag-excluded) Windows machines where the item is still present (apps) or still
enabled at sign-in (startup); `converged` = machines with an `ok`/`not_present` attempt that no
longer have it; `open_failures` uses the `/failures` classification. `machines` lists non-archived
Windows machines; the switch fields are `null` until the machine reports after migration `0020`
(`allowed` is the collapsed flag and is also set by older reports). Excluded machines have nothing
pending.

`linux` is **advisory only**: the Linux agent has no hygiene backend and the server never pushes
anything to a host. Advice is derived from metrics already collected — `advice[].key ∈ {smart,
disk_critical (fullest mount ≥95 % used), disk_low (≥85 %), mem_pressure (7-day mean ≥0.85),
cpu_sustained (7-day mean ≥75 %), long_uptime (≥90 days — installed kernel/library updates need a
restart)}`. CPU/RAM advice is withheld below 12 hourly samples.

### `GET /api/machines/{id}/remediation`  (viewer authorization)

```jsonc
{
  "machine_id": "…",
  "catalog_version": "v1-14da70d24ee0",       // as reported by the agent
  "server_catalog_version": "v1-14da70d24ee0",
  "catalog_stale": false,
  "policy_revision": "7",
  "allowed": true,                             // the local GPO switch permits the action
  "reported_at": "…",
  "items": [ { "kind": "app", "item_id": "weather", "display_name": "Weather (MSN)",
               "risk_tier": "safe", "present": true, "scope": "both", "selected": true,
               "first_seen": "…", "last_seen": "…", "last_present_at": "…" } ],
  "attempts": [ /* RemediationAttemptDTO[], last 50 */ ]
}
```

`enabled` is present only on `kind:"startup"` items (whether it runs at sign-in). `removed_at` is
stamped only on a present→absent transition. Unknown machine → `404`.

---

## 11. First-run setup and rollout (setup wizard)

The server **never holds domain credentials**: there is no field, table or endpoint for that
(docs/SECURITY.md). What rolls the agent out to the domain is the `tolikopa-setup.exe` tool, which
the admin downloads from the panel and runs on **their own** domain PC, under **their own** session.

### 11.1 First run (no identity required)

| Endpoint | Body | Response |
|----|-------|-------|
| `GET  /api/setup/status` | — | `{needs_admin: bool, setup_complete: bool}` |
| `POST /api/setup/admin` | `{setup_code, username, password, display_name?, lang?}` | the **same** body as a successful login (`{username, role, csrf, mfa_enrollment_required}`) + session cookies |

- `needs_admin` = the `admin_user` table is empty. `setup_complete` = an admin has completed the
  wizard (§11.2 `complete`). Both are also returned in `GET /api/settings/public`.
- `POST /api/setup/admin` creates the **first** admin (`role=admin`) and signs them in. It works only
  while the table is empty; otherwise `409 setup_already_done`. Of two concurrent requests, only one
  creates an account (advisory lock).
- **Setup code:** single-use. When there is no account, the server takes the code at startup from
  the `SETUP_CODE` environment variable (≥8 characters), otherwise generates one
  (`XXXX-XXXX-XXXX-XXXX`) and writes it to the **server log** as a prominent block
  (`docker compose logs api`). Case, dashes and spaces don't matter. Wrong code →
  `401 setup_code_invalid`. Attempts are subject to the same increasing lockout as login, per client
  IP (`429 rate_limited`). The code dies once the account is created. A generated code changes on
  every restart.
- `username`/`password`/`display_name` rules are the same as §7.1 / §2.2 (`invalid_request` +
  `params.field`, `password_too_weak`, `password_too_long`). If `lang ∈ {tr,en}` is given, it becomes
  both the user's language preference and the installation's `default_lang`.
- Seeding happens **only while `admin_user` is completely empty** (under the same lock as the setup
  code); on an installation that has any account the environment variables are ignored — a deleted
  account does not come back on restart.
- Seeding at startup via `ADMIN_USERNAME`/`ADMIN_PASSWORD` is still supported but is now
  **optional**; if given, the account is created at startup, `needs_admin=false`, and no code is
  generated.

### 11.2 Wizard (admin)

| Endpoint | Body | Response |
|----|-------|-------|
| `GET  /api/admin/setup` | — | `SetupOverview` (below) |
| `PUT  /api/admin/setup/config` | the `config` fields themselves, not wrapped (may be partial; at least one field) | `200` current `config` |
| `POST /api/admin/setup/complete` | — | `200 {completed_at: "…"}` |
| `POST /api/admin/setup/reopen` | — | `200 {completed_at: null}` |
| `GET  /api/admin/setup/download/{name}` | `?lang=tr\|en` (only for the two tools) | file |

```jsonc
// GET /api/admin/setup
{
  "config": {
    "server_url": "https://tolikopa.corp.example.com",   // "" = not set yet; the agents' backend_url
    "domain_fqdn": "corp.example.com",                   // "" = not set yet
    "netbios": "CORP",                                   // optional
    "target_dn": "",                                     // where the GPO is linked; "" = domain root
    "gpo_name": "Tolikopa Agent",
    "defender_exclusion": false
  },
  "completed_at": null,                                  // RFC 3339 | null
  "ingest_token_masked": "abcd************wxyz",         // the token itself NEVER appears in this body
  "readiness": { "server_url": true, "root_ca": true, "artifacts": true, "domain": true },
  "artifacts": { "product_version": "2.0.0", "agent_version": "1.9.0",
                 "agent_sha256": "…64 hex…", "built_at": "2026-10-13T08:00:00Z" },   // null if absent
  "fleet": { "total": 40,
             "by_agent_version": [ { "version": "1.9.0", "count": 33 }, { "version": "1.8.2", "count": 7 } ],
             "current": 33, "outdated": 7 },
  "update_available": true,
  "server_url_candidates": [ "https://tolikopa.corp.example.com", "https://10.0.0.10" ]
}
```

- `readiness`: `server_url` / `domain` = the corresponding `config` field is set; `root_ca` = the
  panel's TLS certificate file is readable (`TLS_CERT_FILE`, default `/tls/tolikopa.crt` — the
  self-signed certificate generated by `setup.sh`; the field name stayed `root_ca` for backward
  compatibility); `artifacts` = `artifacts.json` + `tolikopa-agent.exe` + `tolikopa-setup.exe`
  + `bootstrap.ps1` exist in `ARTIFACTS_DIR`. None of them depends on any machine having reported.
- `artifacts.agent_sha256` is the SHA-256 of the served `tolikopa-agent.exe` (computed on the server
  at startup / when the file changes). The setup tool prints the same value in its plan; the admin
  compares the two (optionally: `Get-FileHash` on the file copied to the share).
- `fleet`: non-archived **Windows** machines (`kind <> 'linux'` — `tolikopa-update.zip` cannot
  update the Linux agent, so it neither counts as outdated nor raises `update_available`), by `machine.agent_version` (`version: ""` = did not report a
  version). `current` = those on the same version as `artifacts.agent_version` (a leading `v` is
  ignored); `outdated` = the rest. If `artifacts` is absent, both are `0`.
  `update_available = artifacts != null && outdated > 0`.
- `server_url_candidates`: `https://<host>` for each `SERVER_HOST` entry + the address this request
  came in on.

**`PUT /api/admin/setup/config`** — a field that is not sent is unchanged. Validation error:
`400 invalid_request`, `params.field` ∈ `server_url | domain_fqdn | netbios | gpo_name | target_dn`.
The rules are **identical** to the setup tool's own validation (the tool re-validates every field;
the panel rejects before the download would fail on the admin's PC):

| Field | Rule |
|------|------|
| `server_url` | `https://host[:port]` — https only, no path/query/userinfo; lowercased, trailing `/` and `:443` dropped. `""` = clear |
| `domain_fqdn` | `^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$`, ≤253 (at least one dot); **the last label cannot be all digits** (`10.0.0.99`, `corp.123` are rejected — an IP address is not a domain name). `""` = clear |
| `netbios` | empty or `^[A-Za-z0-9-]{1,15}$` |
| `gpo_name` | `^[A-Za-z0-9][A-Za-z0-9 _-]{0,62}$` (cannot be empty) |
| `target_dn` | empty or a DN made of `OU=/DC=/CN=` components (`^(OU\|DC\|CN)=[\p{L}\p{N} ._&()-]+(,…)*$`, ≤512), must end with the DN of `domain_fqdn`; no quotes, escapes, `$`, `;` or backticks |
| `defender_exclusion` | boolean |

**`complete`** requires all four `readiness` values; if any are missing,
`409 setup_incomplete` + `params.missing` (e.g. `["root_ca","domain"]`). It does **not** require any
machine to have reported. `reopen` clears the flag (settings are not deleted).

### 11.3 Downloads — `GET /api/admin/setup/download/{name}`

Response headers: `Content-Disposition: attachment; filename="<name>"`, `Cache-Control: no-store`;
`Content-Type: application/zip` for the two tool zips. The names form a closed set: the former
`tolikopa-setup.exe` / `tolikopa-update.exe` downloads (an exe with the bundle appended) no longer
exist and answer `404`.
Every download is written to the server log (who, which file, client IP).

| `name` | Content | Requirements |
|--------|--------|-----------|
| `tolikopa-setup.zip` | `tolikopa-setup.exe` (unmodified) + `tolikopa.bundle` (**setup**) + `README.txt` | artifacts, `server_url`, `root_ca`, `domain` |
| `tolikopa-update.zip` | the **same** exe bytes as `tolikopa-update.exe` + `tolikopa.bundle` (**update**, does not touch the GPO) + `README.txt` | same |
| `tolikopa-agent.exe` | Windows agent (raw) | artifacts |
| `tolikopa-agent-linux` | Linux agent (raw) | artifacts |
| `bootstrap.ps1` | GPO startup script (raw) | artifacts |
| `config.json` | agent configuration | `server_url`, `root_ca` |

Errors: `503 artifacts_unavailable` (no build artifacts — normal in local development),
`409 setup_incomplete` + `params.missing` (a setting / the TLS certificate is missing, or a stored
field does not satisfy the tool's rules: `missing` carries that field's name), `404 not_found`
(unknown name). If artifacts are missing, `503` is returned regardless of what else is missing.

For the two tools, `?lang=tr|en` goes into the manifest's `lang` field (the tool's console
language); if not given, `default_lang` is used.

**Download and bundle format.** The server carries a single, generic `tolikopa-setup.exe` (with a
Windows version resource and an `asInvoker` manifest, added at image build time). The download is
a zip holding that executable **byte for byte unmodified** — so its SHA-256 is the same for every
installation of a build — next to `tolikopa.bundle`, the installation-specific part, and a short
bilingual `README.txt`. `tolikopa.bundle` is itself a plain zip:
`manifest.json` + `tolikopa-agent.exe` + `bootstrap.ps1` + `config.json`.

The tool, started without `-bundle`, looks for its bundle (1) appended to its own executable
(the layout of older downloads, still accepted) and otherwise (2) as `tolikopa.bundle` in the
**executable's own directory** (`os.Executable` + `EvalSymlinks`) — never in the working directory
or anywhere else. Why the exe is no longer modified: docs/SECURITY.md, "Antivirus and SmartScreen".

```jsonc
// manifest.json — the tool decodes with DisallowUnknownFields; the fields are EXACTLY these
{ "mode": "setup",                       // setup | update
  "lang": "tr",
  "product_version": "2.0.0", "agent_version": "1.9.0",
  "server_url": "https://tolikopa.corp.example.com",
  "domain_fqdn": "corp.example.com", "netbios": "CORP",
  "target_dn": "", "gpo_name": "Tolikopa Agent", "defender_exclusion": false,   // "", "", false for update
  "files": [ { "name": "tolikopa-agent.exe", "sha256": "…" },
             { "name": "bootstrap.ps1", "sha256": "…" },
             { "name": "config.json", "sha256": "…" } ] }
```

The structure is mapped **by hand** between `setup/internal/payload` and `server/internal/bundle`
(the two Go modules do not import each other); the `server/internal/bundle` tests pin the field names
and, in the repository layout, compare the regexes against the setup module's source.

**`config.json`** (the same document in the bundle and standalone):

```jsonc
{ "backend_url": "https://tolikopa.corp.example.com",
  "ingest_token": "…",                    // the ONLY place the ingest token leaves the server
  "tls_ca_pem": "-----BEGIN CERTIFICATE-----\n…",   // the panel's self-signed certificate (TLS_CERT_FILE)
  "intervals": { "vitals_sec": 15, "sample_sec": 60, "post_sec": 60, "smart_min": 45 },
  "top_n": 10, "spool_max_mb": 50, "log_level": "info" }
```

This file **never contains** the `allow_app_removal`, `allow_startup_control`, `enforce_actions` and
`hygiene_hours` keys: the server cannot set endpoint hygiene's local kill switches (CLAUDE.md
"Safety", invariant 4). The setup tool asks the admin for them **locally** and preserves the values
of the existing `config.json` on the share.

---

All DTO definitions live in `server/internal/model/api.go` (single source of truth).
