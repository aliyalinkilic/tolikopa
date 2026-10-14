# Database (TimescaleDB 2.27 / PostgreSQL 17)

The full DDL lives in `server/internal/db/migrations/*.sql`; the server applies the files in order
at startup, in autocommit mode (per statement), and tracks them in the `schema_migrations` table.
**Docker image:** `timescale/timescaledb:2.27.0-pg17` (standard Alpine; the core design does not
require the Toolkit — `usage_session` and the network deltas are computed in Go. The Toolkit is an
optional future upgrade).

## Tables

**Regular tables:** `machine` (inventory; `ip_addrs` holds the local IPv4 addresses reported by the
agent, `last_ip` the source address of the ingest request — 0012), `machine_live` (upserted on every
batch — the live read path), `admin_user` (panel accounts — see below), `revoked_session` (token IDs revoked on sign-out), `app_setting` (installation-wide settings), `process_category` (admin-editable classification), `disk_health`, `usage_session`
(derived active-usage sessions), `alert_rule`, `alert`, and the endpoint hygiene tables
(`remediation_item`, `remediation_policy`, `remediation_policy_item`, `remediation_policy_audit`,
`machine_remediation_state` — see below).

**Hypertables:**

| Table | Chunk | Compress after | Retention | Note |
|-------|-------|----------------|-----------|------|
| `metric_sample` | 1 day | 7d | 90d | machine vitals + network delta |
| `disk_usage_sample` | 1 day | 7d | 90d | per mount |
| `process_sample` | 12 hours | 3d | 21d | highest volume; top-N |
| `event` | 7 days | — | 365d | crashes / hardware |
| `remediation_attempt` | 7 days | — | 365d | endpoint hygiene audit trail |

**Critical constraints:**
- Every hypertable UNIQUE/PK **includes `ts`** → `ON CONFLICT (…, ts) DO NOTHING` idempotency.
- Compression `segmentby = machine_id` (cardinality ≤50). **Never** segment by `process_name`.
- Reserved word: **`logon_user`** (not `current_user`).

## Continuous aggregates

| View | Source | Bucket | Used for |
|------|--------|--------|----------|
| `metric_5m` | raw | 5min UTC | machine chart (medium range); dashboard `history`; hierarchical parent |
| `metric_1h` | `metric_5m` | 1h UTC | machine chart (long range) |
| `net_user_1h` | raw | 1h **Europe/Istanbul** | per-user network upload/download timeline |
| `proc_cat_1h` | raw | 1h UTC | per-machine category breakdown |
| `proc_cat_user_1h` | raw | 1h **Europe/Istanbul** | per-user program-type breakdown |
| `app_focus_user_1h` | raw | 1h **Europe/Istanbul** | per-user/per-application foreground time |
| `session_user_1h` | raw | 1h **Europe/Istanbul** | session-state sample counters (busy-hours heatmap) |
| `proc_name_1h` | raw | 1h UTC | fleet-wide application consumption (top-apps) |

**Real-time aggregation:** since TimescaleDB 2.13, caggs default to `materialized_only=true`
(materialized data only). Migration 0010 enables real-time aggregation for `metric_5m`,
`session_user_1h` and `proc_name_1h` with `ALTER MATERIALIZED VIEW … SET
(timescaledb.materialized_only = false);`: raw data newer than the watermark is computed on the fly
at query time and merged in (for dashboard freshness). The other caggs stay materialized-only.
**The live fleet view + WS are always fed from `machine_live`, never from a CAgg** (to avoid the
cost of scanning past the watermark).

**`session_user_1h` design:** groups by `(bucket, machine_id, COALESCE(logon_user,''))`; rows with
an empty/NULL `logon_user` **are included** under `''`, so that a single cagg serves both the user
dimension (filtered with `logon_user <> ''`) and machine-level fleet activity. State counters:
`active_samples` / `idle_samples` / `locked_samples` (`count(*) FILTER`) + `total_samples`.
The full `session_state` set: `active | idle | locked | disconnected | none` — `idle` (≥5 min
without input) is never counted as "active" anywhere; in `usage_session` derivation it only counts
toward `logged_on`.

**Sample→seconds rule:** the agent's nominal cadence is **15 s** for vitals (`vitals_sec=15`) and
**60 s** for the full sample carrying the process rows (`sample_sec=60`). Rollups COUNT samples;
every API query that converts them to a duration applies the same multiplier:
`metric_sample`-based seconds = samples × 15 (e.g. `active_seconds` in `hourly-activity`),
`process_sample`-based duration ≈ samples × 60. `proc_name_1h.machines` =
`count(DISTINCT machine_id)` (within a bucket; it cannot be summed across buckets, so the API takes
the peak value).

Hierarchical rollups (metric_5m→1h) use **fixed-width UTC** buckets. The user-facing tz-aware
rollups (`net_user_1h`, `proc_cat_user_1h`) are variable-width, so they are built **directly from
raw** data and cannot be hierarchical children.

## Network delta and resets

The agent sends **cumulative** NIC counters + `boot_time`. At ingest, the previous cumulative value
+ boot time is read for each machine; the delta is:
- boot changed → reboot: `delta = current`
- same boot, `current ≥ prev`: `delta = current - prev`
- same boot, `current < prev`: counter reset/wrap: `delta = current`
- the agent's first sample: `delta = NULL` (excluded from totals)

`net_*_delta` is written to the raw row; rollups are a plain `SUM(delta)`.

## Program-type classification

`process_category` (admin-editable). At ingest it is resolved with an in-memory cache as
`exact→prefix→glob` (lower priority wins), and `category_id` is denormalized. Unknown → NULL.
Reclassification applies going forward; `/api/admin/reclassify` re-resolves the raw window (21d).

## Endpoint hygiene (0011_remediation.sql)

**Authority model:** the agent carries a **compiled-in** allowlist (`agent/internal/appcatalog`)
and a local GPO kill switch. The server can only SELECT from this vocabulary; an unrecognised
`item_id` is rejected by the agent and reported as `outcome:"unknown_item"`. These tables are a
**UI mirror and an audit trail, not a command channel**.

| Table | Role |
|-------|------|
| `remediation_item` | PK `(kind,item_id)`. A mirror of the agent's compiled catalog; **NOT seeded by a migration** — `appcatalog.Sync()` writes it at startup and marks items dropped from the vocabulary `deprecated=true` (it never DELETEs). |
| `remediation_policy` | Singleton (`id int PK DEFAULT 1` + `CHECK (id=1)`, like `derive_state` in 0005). `enabled=false`, `dry_run=true` **by default** — a fresh installation never takes action. `revision bigint` is incremented on every write. |
| `remediation_policy_item` | PK `(kind,item_id)` — the selected subset. There is **deliberately no FK** to `remediation_item`: otherwise an agent newer than the server would make ingest fail with a 500. Validation happens on the admin write path (400). |
| `remediation_policy_audit` | `bigserial`, `ts`, `actor` (JWT Subject), `actor_ip`, `revision`, `before`/`after` jsonb. The permanent record of who enabled a fleet-wide destructive capability; no retention. |
| `machine_remediation_state` | A flat "current state" table like `machine_live`. PK `(machine_id,kind,item_id)`, FK to `machine` `ON DELETE CASCADE`. Partial index `(kind,item_id) WHERE present`. |

**Catalog copy:** the canonical copy of `catalog.json` lives in `agent/internal/appcatalog/`; since
Go embed cannot cross the module boundary (`..` is forbidden in embed patterns), the server copy is
**generated**:
`docker run --rm -v "$PWD:/repo" -w /repo/server golang:1.26 go run ./tools/synccatalog`. CI runs
the same command and treats any drift as an error via
`git diff --exit-code server/internal/appcatalog/catalog.json` (the `-check` flag verifies without
writing). The version hash is computed from the `kind:item_id` pairs only — edits to the Turkish
labels do not invalidate deployed agents.

Added to the `machine` table: `remediation_allowed bool` (whether the local GPO switches permit
action — `enforce AND (apps OR startup)`), `remediation_catalog_version text`,
`remediation_policy_revision text`, `remediation_reported_at timestamptz`.

**`remediation_attempt`** is a hypertable: `create_hypertable(ts, 7 days)`, UNIQUE
`(machine_id, ts, kind, item_id, action)` (every hypertable key includes `ts` → idempotent),
indexes `(kind,item_id,ts DESC)` and `(outcome,ts DESC)`, retention 365d, **no compression**
(same rationale as `event`: the volume is negligible and compressed chunks make late-arriving spool
records harder to handle). `error_code` is only a CODE (`0x80073cfa`) — never a message or a path.

**Full-snapshot semantics:** ingest writes the rows present in the report with a single `unnest`
upsert, then sweeps that machine's `present` rows that are **not** in the report, setting
`present=false` and stamping `removed_at` on the present→absent transition. If the `present` array
arrives empty, no sweep is done: the field is `omitempty`, and a report carrying only `attempts`
would otherwise wipe the whole mirror.

**PRIVACY:** `machine_remediation_state` holds the presence of **allowlisted items only**; it is
**not** a general installed-software inventory (this is also recorded as a `COMMENT` on the table).

## Panel accounts and settings (0015_panel_accounts.sql)

`admin_user` holds the accounts that **can sign in** to the panel; they are unrelated to the
monitored domain users (`logon_user`). Columns added in 0015:

| Column | Type | Note |
|--------|------|------|
| `display_name` | `text NOT NULL DEFAULT ''` | display name |
| `prefs` | `jsonb NOT NULL DEFAULT '{}'` | `{"theme":"dark"\|"light"\|"system","lang":"tr"\|"en"}`; a missing key = the default |
| `last_login_at` | `timestamptz` | last successful sign-in (for a 2FA account, after the second step) |
| `session_epoch` | `int NOT NULL DEFAULT 0` | embedded in the session JWT; if the value in the row differs, the token is rejected. A password change/reset, disabling the account and enabling 2FA **increment** it → the user is signed out everywhere |
| `totp_secret_enc` | `bytea` | the active TOTP secret, **AES-256-GCM** (`nonce‖ciphertext`); the key is derived from `JWT_SECRET` with HKDF-SHA256, AAD = `totp:<id>` |
| `totp_pending_enc` | `bytea` | a secret whose enrolment has started but is not yet verified (same encryption) |
| `totp_enabled` | `boolean NOT NULL DEFAULT false` | |
| `totp_last_step` | `bigint NOT NULL DEFAULT 0` | the last accepted time step (`unix/30`); a code for a step ≤ this value is rejected (replay protection). The update is conditional (`WHERE totp_last_step < $new`) |
| `recovery_codes` | `text[] NOT NULL DEFAULT '{}'` | **HMAC-SHA256** digests of the unused recovery codes (hex; key derived from `JWT_SECRET`). A code is removed with `array_remove` once used — the plaintext is never stored |

The token also carries `admin_user.id`: an account that is deleted and re-created with the same name
is a **different** account, and an old cookie cannot open it. **0016_username_ci_unique.sql** creates
a unique index on `lower(username)` (`admin_user_username_lower_idx`) — only if there are **no**
accounts that differ solely by letter case; if there are, the migration skips without failing, the
server logs a warning on every startup, and creates the index itself once the conflict is resolved
(`auth.EnsureUsernameIndex`). The original `username` UNIQUE constraint is case-sensitive; on create, the API
also rejects a `lower(username)` collision.

**`revoked_session`** (0017_revoked_session.sql) — `jti text PRIMARY KEY, expires_at timestamptz NOT NULL`.
The IDs of session tokens that have been signed out; authentication rejects a token on the list.
A row is only needed until the token itself expires; expired rows are deleted on every sign-out.

**`app_setting`** — `key text PRIMARY KEY, value jsonb NOT NULL, updated_at timestamptz`. Small
installation-wide settings; **no secrets are stored** (some keys are served by the unauthenticated
`GET /api/settings/public`). The migration inserts the defaults (`ON CONFLICT DO NOTHING`):

| `key` | `value` | Note |
|-------|---------|------|
| `org_name` | `""` | organisation name (login page) |
| `default_lang` | `"tr"` | `tr`\|`en`; the UI default + the language of alert e-mails and the stored `alert.message` |
| `require_2fa` | `false` | mandatory 2FA enrolment for all panel accounts (docs/API.md §2.4) |

These three are the **general settings** (`GET/PUT /api/admin/settings/general`; the
unauthenticated `GET /api/settings/public` serves all three). The setup wizard keeps two more keys
in the same table; they are not inserted by a migration but created on first write (while the row
is missing, the defaults apply):

| `key` | `value` | Note |
|-------|---------|------|
| `setup_config` | object: `{"server_url","domain_fqdn","netbios","target_dn","gpo_name","defender_exclusion"}` | rollout settings (docs/API.md §11.2). While the row is missing: all strings `""`, `gpo_name` = `"Tolikopa Agent"`, `defender_exclusion` = `false`. **Contains no credentials** — the server holds no domain username/password. Written only by `PUT /api/admin/setup/config` (with the tool's validation rules) |
| `setup_completed_at` | RFC 3339 string or `null` | the moment the wizard was completed; missing row / `null` = not completed (`setup_complete=false`). `complete` writes it, `reopen` sets it to `null` |

The ingest token, the setup code and the panel's TLS certificate are **not** kept in this table
(they are, respectively, an environment variable, in memory only, and a file).

**Alert parameters.** There is no new column: `alert.detail` (jsonb) now carries **all** the values
that go into the rule's message (`hostname`, `threshold`, `unit`, … — docs/API.md §6), and the API
exposes them as `params`. `alert.message` is a fallback text generated in `default_lang`. The
`detail` of older rows is partial; the API fills in the missing values at read time (except
`threshold`).

## Machine archive (0013_machine_archive.sql)

| Object | Note |
|---|---|
| `machine.archived_at`, `machine.archived_by` | Set = the machine has been decommissioned. Current-state queries filter with `m.archived_at IS NULL`; historical hypertable/cagg data is not deleted. Partial index `WHERE archived_at IS NOT NULL`. |
| `machine_archive_audit` | Permanent audit: `archive` / `restore` / `auto_restore` (when ingest arrives from an archived machine), `actor`, `actor_ip`. Never pruned. |

When writing a new "current state" query (a list/count over `machine` or `machine_live`), adding the
archive filter is mandatory; without it, retired machines reappear in counts and alerts.

## Timezone

Everything is stored as `timestamptz` (UTC). User-facing day/hour buckets use `Europe/Istanbul`
(Turkey has been on permanent UTC+3 since 2016, no DST).

## Backups

Logical: `pg_dump` (following TimescaleDB's dump/restore steps — CAggs + policies are preserved) or
`pgBackRest` PITR. Run a hypertable + CAgg restore test before go-live.
