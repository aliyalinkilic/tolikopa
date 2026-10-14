# Tolikopa Agent

A single static Go binary. On Windows it is a LocalSystem service, on Linux a systemd unit. It has
no installer UI; it is installed silently via GPO (Windows) or `setup.sh` (Linux, the server itself).

## Privacy allowlist — binding

The agent collects **only** the fields below. Nothing outside them is read, sent, or logged:

**Allowed:** CPU/RAM/disk gauges · NIC total byte counters · process **name + category +
cpu% + ram** · the logged-on **username** · session state (active/idle/locked/…) · the
**seconds** elapsed since the last keyboard/mouse input (duration only — never content) · the
foreground application's **base name + duration** (never the window title/document/URL) · disk
SMART health · application crash event (the crashing application's **base name** + exception code
+ time) · presence information for hygiene items **in the compiled catalog only** (see below) ·
the machine's **own local IPv4 addresses** (identity information like the hostname; read from the
interface configuration — never sockets, peers, or traffic).

**Forbidden (never):** screen contents/screenshots · keystrokes · mouse coordinates · window
titles · foreground-window text · visited URLs · browser history · DNS/packet contents · file
names/paths · document contents · process **command line** · environment variables · open file
handles · per-process network connections.

At the code level: `gopsutil` process code calls **only** `.Name()`, `.Times()`, `.MemoryInfo()`,
`.CreateTime()`. `.Cmdline()`, `.Exe()`, `.Environ()`, `.OpenFiles()`, `.Connections()`,
`.Cwd()` are **not used**. The `security-privacy-reviewer` agent audits this on every change.

## Collected metrics and APIs used

| Metric | Source (Windows) | Source (Linux) |
|--------|------------------|----------------|
| CPU % (total+per-core) | `gopsutil/cpu.Percent` (warmed up at startup) | same |
| RAM used/total | `gopsutil/mem.VirtualMemory` | same |
| Disk used/total + I/O | `gopsutil/disk.*` (fixed disks only) | same |
| NIC upload/download | `gopsutil/net.IOCounters(true)` (cumulative) | same |
| Top-N processes (name+cpu+ram) | `gopsutil/process` + PID→lastCPUTime map | same |
| Active user | `WTSGetActiveConsoleSessionId`+`WTSQuerySessionInformation` | `utmp`/`who` (loginctl) |
| Session state | `svc.AcceptSessionChange` + WTS_SESSION_LOCK/UNLOCK | session present/absent |
| Activity / idle | `GetLastInputInfo` from the in-session helper (duration only) | none → "active" if a session exists |
| Disk SMART | WMI `MSFT_PhysicalDisk.HealthStatus` (+ `MSStorageDriver_FailurePredictStatus`) | no `gopsutil` → best-effort |
| Application crash | `wevtapi` Application channel, EventID 1000/1002/1001 | (disabled on Linux) |
| Local IPv4 addresses | stdlib `net.Interfaces()` (up, excluding loopback/169.254, ≤8) — in every batch | same |

**Collection cadence:** vitals 15s (avg/max in the batch), disk+net+top-N 60s, SMART/WMI 30-60min,
event log via real-time subscription. **Batch POST every 60s** (or N items). Heartbeat even when idle.

## Session state and idle detection

`session_state` takes these values: **`active | idle | locked | disconnected | none`**.

- **`idle`** (Windows only): the console session is open and NOT locked, but there has been no
  keyboard/mouse input for `idle_threshold_sec` (default **300 s**) or longer.
  `logon_user` stays populated while idle. `locked`/`disconnected`/`none` always take precedence.
- Data source: the `--fg-helper` helper, which the SYSTEM service launches in the user session,
  reads the **seconds elapsed since the last input** with `GetLastInputInfo` and adds it to every
  report line as the control field `"__idle": <seconds>`. Keys prefixed with `__` are reserved
  protocol control fields, never application names; the helper writes a line every round (~30 s)
  even when there is no application to report (e.g. `{"__idle":420}`) so the data stays fresh.
- **Privacy:** this measurement is only "when was the last input" — a freshness counter. Which key
  was pressed, what was typed, mouse coordinates, and window/screen contents are never read; the
  allowlist's prohibitions apply unchanged.
- **Fallback:** if idle data is missing or stale (the last report is older than 90 s — the helper
  crashed, the session was just opened, the machine just started), the agent reverts to the
  current behaviour and reports `active`. On Linux (headless server) there is no idle detection;
  it stays `active` if a session exists.
- The threshold is set with `idle_threshold_sec` in `config.json`; values below 60 are clamped to
  60, and 0/missing uses the default 300.

## Endpoint hygiene (bloatware cleanup and logon startup control)

The agent reports the state of items **in the compiled catalog**
(`agent/internal/appcatalog/catalog.json`) on the machine and — only if authorised — removes/disables
them. Inventory is always on and **read-only**; the only thing that can be turned off is **action**.

### What is collected (allowlist)

| Field | Content |
|------|--------|
| `catalog_version` | Hash of the catalog vocabulary (e.g. `v1-14da70d24ee0`) |
| `present[].item_id` | Item key **from the catalog** only (e.g. `weather`, `startup.spotify`) |
| `present[].scope` | Apps: `user` \| `provisioned` \| `both` |
| `present[].enabled` | Logon startup entries: whether it currently runs at startup |
| `attempts[]` | Result of the action taken/not taken + **code only** (such as `0x80073cfa`) |
| `allowed` | State of the three local kill switches (`apps`/`startup`/`enforce` — see below) |
| `policy_revision` | The policy revision the agent applied (an opaque string sent by the server) |

**Never collected:** general software inventory (programs the user installed themselves are never
visible to Tolikopa at all) · package version/publisher/install path · user SID or name · the
**data** field of the Run entry (it is a file path — a forbidden class) · error message texts.

No package or startup entry that is not in the catalog **can enter** the report; the catalog is
embedded in `tolikopa-agent.exe`, so extending the vocabulary is done not by writing to a database
but by deploying a new agent through the audited GPO path.

### How it is read (Windows)

| Source | API |
|--------|-----|
| Appx package **presence hint** (all users + image) | `HKLM\…\Appx\AppxAllUserStore` (read-only registry) |
| Appx package **actual presence and identity** | `PackageManager.FindPackagesByPackageFamilyName` (WinRT, in a short-lived child process, SYSTEM) |
| Logon startup entries | `HKLM\…\Run`/`RunOnce`, `HKLM\SOFTWARE\Wow6432Node\…\Run`, `HKU\<SID>\…\Run` — **value names** only |
| Enabled/disabled state | `…\Explorer\StartupApproved\Run` (12-byte state blob) |

> **What `AppxAllUserStore` cannot answer.** The subkey names under a user SID look like
> `PackageFullName`s, but this key is not a live package list — it is a **deployment log**: it
> accumulates one record for every version ever deployed (25 Spotify versions on the diagnostic
> machine), does not delete records after a package is removed, and the version that is actually
> registered may not be in the list at all. It is therefore used only for the question "was this
> catalog family ever deployed on this machine". Removal targets are **not derived from it** —
> before 2026-09 they were, and every removal attempt failed with `0x80070490` (`ERROR_NOT_FOUND`).
>
> At the start of every hygiene pass the agent queries the deployment API for **all catalog
> families in a single child process**. This one read determines two things at once: the inventory
> reported to the panel (records the registry hint contradicts are dropped; only records in the
> *provisioned* scope are kept, because they are image-level records) and the removal targets. If
> the read **cannot be done**, the inventory falls back to the registry hint and removal is
> deferred (`0xE0000005`) — it is never reported as "machine clean".

### What can be done

> Appx removal is **enabled** after the WinRT binding constants (`IPackageManager` /
> `IPackageManager2` / `IPackageManager8` IIDs, `IIterable`/`IIterator`/`IPackage`/`IPackageId` IIDs,
> vtable slot indices, `RemovalOptions.RemoveForAllUsers = 0x80000`) were verified against the
> Windows SDK 10.0.26100 headers (`agent/internal/actuator/winrt_constants_windows.go`).
> If the table is left incomplete again, the worker refuses without making any COM call and the
> attempt fails with `0xE0000001` — this is deliberate fail-closed behaviour. The same fail-closed
> rule applies to package resolution: if the deployment API cannot be queried (e.g. `0x80070005`,
> in a non-administrator context), no partial answer is accepted even for a single family, the
> whole resolution is treated as failed and the attempt returns `0xE0000005`. The deprovision step
> requires `IPackageManager8` (Windows 10 1809+); on older versions it fails with
> `E_NOINTERFACE`.

- **Disable/enable a logon startup entry:** the `StartupApproved` value that Windows' own "Startup
  apps" screen writes is written. The Run entry is **never deleted** → the operation is reversible.
- **Appx package removal:** first, the list of `PackageFullName`s of the package family that are
  **actually deployed** at that moment is read from the deployment API
  (`FindPackagesByPackageFamilyName`; the overloads that take no user SID cover all users and
  require administrator rights — the worker runs as SYSTEM), then each one is removed for all users
  and then *deprovisioned* (`…\AppxAllUserStore\Deprovisioned\<PackageFamilyName>`); otherwise
  Windows Update and new user profiles bring the package back. Removal runs in a **short-lived child
  process** that the agent itself launches (`tolikopa-agent.exe --appx-worker`, terminated after
  180 s): a call that talks to AppXSvc can hang for minutes, and in a service that runs for months
  this has to be isolated at the process boundary.
- **On Windows 11 Pro removal is "best-effort".** `ContentDeliveryManager` reinstalls consumer apps
  (Solitaire, TikTok, Spotify…) into user profiles, and some inbox apps also come back with feature
  updates. Therefore the agent makes **at most 3 attempts** per item, then waits **24 hours**
  (`state/hygiene_budget.json`): the goal is not to wrestle with the operating system forever and
  not to bloat the audit log. A permanent result requires the relevant policies on the
  Enterprise/Education editions.

### Authorisation and kill switches

The server can only **select from the catalog**; an item it does not recognise is rejected locally
and reported back with the `unknown_item` result. Authority to act lies entirely with the client
(`config.json`, deployed via GPO) and is **off** by default:

| Key | Default | Effect |
|---------|-----------|--------|
| `allow_app_removal` | `false` | Permission to remove apps |
| `allow_startup_control` | `false` | Permission to disable startup entries |
| `enforce_actions` | `false` | While `false`, the agent only evaluates and reports `dry_run` |
| `hygiene_hours` | `6` | Inventory/reconciliation frequency (hours, minimum 1) |

Decision order (exactly this, so the meaning of a result is never ambiguous): not in the catalog →
`unknown_item`; the relevant switch is off → **no record at all** (the permission state is reported
at machine level as `remediation_allowed`, shown in the panel as the "No GPO Permission" tile);
protected package → `blocked`; already absent/already disabled → `not_present` (this is also the
result when Windows says `0x80070490`: the desired state is that the package is gone, and it is
gone — this is not a failure and does not consume the attempt budget); policy off → no record at
all; `dry_run` or `enforce_actions=false` → `dry_run`; otherwise the action.

> **Important consequence:** a `dry_run` row only comes from machines whose local permission is
> on. The panel's "Enforce" mode lock requires at least one `dry_run` result; therefore the lock is
> never released until the `allow_*` switches are turned on via GPO. The inventory ("installed on N
> machines") is collected independently of the permission and does not substitute for an attempt
> result. The policy reaches the agent **in the ingest response** (the 202 body), is validated
locally and written to `state/policy.json`. The policy in the response to a batch **resent from
the spool** is never applied — that batch may date from hours earlier.

## Service lifecycle (Windows)

`main.go` determines the mode with `svc.IsWindowsService()`:
- Under the SCM → run as a service.
- `install|uninstall|start|stop` → service management via `mgr` (delayed-auto-start, LocalSystem,
  recovery=restart).
- `run` → in the foreground (development/testing).
- `--fg-helper` → helper running in the user session: counts duration per foreground application
  **name** and reports the seconds since the last input in the `"__idle"` field (see above).
- `--idle-helper` → old draft; its function moved to `--fg-helper`, it exits cleanly.

`Execute`: START_PENDING → load config, warm up the CPU sampler, load/create `agent_id`, register
for session+power notifications → **report RUNNING quickly** (SCM ~30s timeout; slow init in
goroutines) → collector + sender start. On Stop/Shutdown the context is cancelled and the spool
flushed.

## Identity and state

Under `C:\ProgramData\Tolikopa\` (Linux: `/var/lib/tolikopa/`): `config.json`, `state/agent_id`,
`spool/`, `logs/`. `agent_id` is a UUID generated once (survives renames). hostname via
`GetComputerNameEx`, domain via `NetGetJoinInformation`, OS build via `RtlGetVersion`.

## Sender

gzipped JSON, `Authorization: Bearer`, `Idempotency-Key`. On failure, an NDJSON buffer in `spool/`
(cap ~50MB, drop the oldest). 4xx (bad token) → stop+log; 5xx/network → exponential backoff.
`2xx` → delete the spool file.

## Configuration (`config.json`)

```jsonc
{
  "backend_url": "https://10.0.0.10",
  "ingest_token": "<distributed via GPO>",
  "tls_ca_pem": "",              // the panel's self-signed certificate (pinned; filled in by the panel); if empty, the system trust store
  "intervals": { "vitals_sec": 15, "sample_sec": 60, "post_sec": 60, "smart_min": 45 },
  "top_n": 10,
  "spool_max_mb": 50,
  "idle_threshold_sec": 300,     // an open session with no input for this many seconds counts as "idle" (min 60)
  "log_level": "info",

  // Endpoint hygiene — all three default to false and a missing value is NEVER turned into true:
  // leaving it empty is a deliberate "no". Inventory is always collected (read-only);
  // only action is enabled by these switches.
  "allow_app_removal": false,     // permission to remove the Appx packages in the catalog
  "allow_startup_control": false, // permission to disable the logon startup entries in the catalog
  "enforce_actions": false,       // false: only evaluate and report "dry_run", change nothing
  "hygiene_hours": 6              // inventory/reconciliation frequency (hours, floor 1)
}
```

ACL is SYSTEM+Administrators only. Because the token sits on the client's disk, it is
ingest-scoped only and can be rotated (see SECURITY.md).

## Self-update

The service never overwrites its own running image. `bootstrap.ps1`, invoked by the GPP task,
compares the FileVersion of `tolikopa-agent.exe` in NETLOGON; if it is newer, it stops the service,
replaces the exe, and starts it again (see GPO-DEPLOYMENT.md).
