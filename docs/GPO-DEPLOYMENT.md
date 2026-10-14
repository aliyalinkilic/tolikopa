# Windows Server 2016 — Agent Deployment via GPO

The agent is deployed to every domain-joined PC **without any installer UI**, via Group Policy.
The mechanism: the payload is kept in NETLOGON; a **GPP Scheduled Task (Update mode)** runs the
idempotent `bootstrap.ps1` as SYSTEM shortly after Group Policy registers it, at startup and daily; the script
installs/updates/repairs the Windows service. This gives both GPP's self-healing behaviour and a
real service (SESSIONCHANGE + automatic restart).

## Recommended path: `tolikopa-setup.exe`

The panel's setup wizard (or the **Setup** page) produces a `tolikopa-setup.exe` specific to this
installation. Run it on a domain-joined computer (with the Group Policy management tools
installed) or on a domain controller:

1. It asks for the panel's DNS name and IP (below), lists what it will do (with `-dry-run` it
   only lists) and asks for confirmation.
2. It copies the files `tolikopa-agent.exe`, `bootstrap.ps1`, `config.json` to
   `\\<domain>\NETLOGON\Tolikopa`.
3. It creates the panel's **DNS record**.
4. It creates the GPO: the scheduled task below, **trust in the panel certificate**, and an
   optional Defender exclusion.
5. It links the GPO to the target OU.

Required rights: write access to NETLOGON, GPO creation (Domain Admins or Group Policy Creator
Owners), linking GPOs on the target OU, and adding DNS records/zones (Domain Admins or DnsAdmins).
The tool runs under the session of the person running it; no domain password is entered into the
panel or the server. It is safe to run again.

For a new agent version the panel offers `tolikopa-update.exe`: it only refreshes the files in
NETLOGON; the DNS and certificate steps exist only in `tolikopa-setup.exe`.

### DNS record

The tool reads the panel certificate from the `config.json` in the package (`tls_ca_pem`) and
asks for the DNS name and IP; the defaults in square brackets come from the certificate (the first
DNS name and the first IPv4 address). Enter accepts the default; typing `-` at the name prompt
skips the DNS step. The name must be one of the names the certificate covers (otherwise it is
asked again); an IP the certificate does not cover only produces a note (the browser warns when
that IP is used as the address).

The record is created with the DnsServer PowerShell module on the domain controller holding the
PDC role:

- If a primary forward lookup zone contains the name (e.g. `corp.local` for
  `tolikopa.corp.local`), an **A record** is added to that zone.
- If no zone contains the name (e.g. `tolikopa.local`), an Active Directory-integrated primary
  zone is created **with exactly this name** (`ReplicationScope Domain`) and an A record is added
  at the zone root. A new zone makes the domain's DNS answer for that whole name space, so it is
  **only created interactively**: the tool asks you to type the zone name before the general
  confirmation. With `-yes` or `-dry-run` a name that needs a new zone is refused — run
  interactively, use `-dns-name` for a name inside an existing zone, or use `-skip-dns`.
- If the name already points to another address (or has a CNAME/AAAA record), it is **not
  overwritten**: the tool shows the existing addresses and stops. Choose another name, delete the
  old record, or use `-skip-dns`.

If the tool runs somewhere other than a DC, the DNS module is required (elevated PowerShell):

```powershell
Add-WindowsCapability -Online -Name Rsat.Dns.Tools~~~~0.0.1.0
```

### Certificate trust

If the certificate can be distributed safely, the tool adds it to the same GPO:
`HKLM\Software\Policies\Microsoft\SystemCertificates\Root\Certificates\<SHA1>\Blob`
(Registry CSE). Computers the GPO applies to open the panel without a warning. Conditions: the
certificate is self-signed, **not a CA** (no CA flag / certificate-signing right), has
`serverAuth` usage, names **exactly one DNS host** (no wildcard) and at most one IP address,
belongs to the bundle's server (its DNS name is the host of the server URL, or — when the server
URL is an IP address — its IP address is that address) and has not expired — this is the
certificate `setup.sh` generates. If any condition fails, the tool prints the reason and does not distribute the
certificate (rationale: [SECURITY.md](SECURITY.md)).

When the certificate changes, running again removes the old fingerprints from that GPO. Removing
the GPO also removes the trust. Computers outside the GPO's target OU (e.g. administrator PCs in
another OU) do not receive the trust; add the certificate to them by hand (DEPLOYMENT.md → TLS).

### Parameters

| Flag | Meaning |
|------|---------|
| `-dry-run` | only shows the plan, changes nothing |
| `-yes` | asks for no confirmation; uses the defaults or flags for the DNS name/IP |
| `-dns-name <name>` | the panel's DNS name (default: the name in the certificate) |
| `-dns-ip <ip>` | the address the name will point to (default: the address in the certificate) |
| `-skip-dns` | does not create/check the DNS record |
| `-remove` | deletes the GPO (and with it the certificate trust) and the NETLOGON folder |

`-remove` **does not delete the DNS record**; delete it by hand if needed (DNS Manager or
`Remove-DnsServerResourceRecord`). It also does not remove agents already installed on the
computers.

The sections below are for those who want to do the same work **by hand**, and for
troubleshooting.

## Prerequisites

- The Ubuntu side is installed and the panel is reachable (`https://10.0.0.10`).
- At hand: `tolikopa-agent.exe` (built), `bootstrap.ps1`, `config.json` (with backend_url +
  ingest_token + tls_ca_pem filled in). You can download these from the panel's Setup page.

## 1) Put the payload in NETLOGON

On the DC (SYSVOL replicates to all DCs; machine accounts can read it → no extra share permission):

```powershell
$dst = "\\CORP\NETLOGON\Tolikopa"
New-Item -ItemType Directory -Force -Path $dst | Out-Null
Copy-Item .\tolikopa-agent.exe, .\bootstrap.ps1, .\config.json -Destination $dst -Force
```

Example `config.json` (downloaded ready-made from the Setup page):
```json
{ "backend_url": "https://10.0.0.10", "ingest_token": "…", "tls_ca_pem": "-----BEGIN CERTIFICATE-----\n…" }
```

### Endpoint hygiene switches (optional, off by default)

The four keys below come **only from the `config.json` distributed via GPO**; the panel cannot
change them. When any of the three switches is left empty/missing it is taken as `false`, and that
is a deliberate "no": the agent changes nothing on the machine and only reports the state of the
catalog items (a read-only inventory). **While permission is off, the agent does not send dry-run
(`dry_run`) results either**; since the panel's "Enforce" mode lock requires at least one dry-run
result, that mode cannot be selected at all until the switches are turned on.

When you want removal/disabling to happen, turn on the relevant switch **and** `enforce_actions`
together (`deploy/gpo/config.example.json` ships like this):

```json
{
  "allow_app_removal": true,
  "allow_startup_control": true,
  "enforce_actions": true,
  "hygiene_hours": 6
}
```

With this setting the machine **still does nothing**; as long as the operating mode in the panel
is "Dry Run" (the default), the agent only evaluates and reports `dry_run`. Actual removal starts
only when "Enforce" is selected and saved in the panel; that switch is in turn locked on the
server to at least one observed dry-run result.

A more cautious path: deploy to a pilot OU with `enforce_actions: false` — even if the panel is
set to "Enforce", those machines keep reporting `dry_run` — then set it to `true` and redeploy.

When `config.json` changes, `bootstrap.ps1` **restarts** the service (the agent reads its config
only at startup); the new keys take effect on the GPO task's next trigger. Details:
`docs/AGENT.md` → "Endpoint hygiene".

## 2) Create the GPO

1. **Group Policy Management** (gpmc.msc) → domain → right-click → **Create a GPO in this
   domain…** → name: `Tolikopa Agent Deployment`.
2. Right-click the GPO → **Edit**.

## 3) Add a GPP Scheduled Task

`Computer Configuration → Preferences → Control Panel Settings → Scheduled Tasks` → right-click →
**New → Scheduled Task (At least Windows 7)**.

- **General:**
  - Action: **Update**
  - Name: `TolikopaAgent`
  - "When running the task, use the following user account": `NT AUTHORITY\SYSTEM`
  - **Run whether user is logged on or not**
  - **Run with highest privileges**
  - **Hidden** checked
  - Configure for: **Windows Vista / Windows Server 2008** (enables the SYSTEM+Hidden options)
- **Triggers:** add two triggers → **At startup**, and **Daily** (once a day; a 1-hour repeat can
  also be added).
- **Actions:** New → Start a program:
  - Program: `powershell.exe`
  - Arguments:
    `-WindowStyle Hidden -NonInteractive -ExecutionPolicy Bypass -File \\CORP\NETLOGON\Tolikopa\bootstrap.ps1`
- **Settings:** "Allow task to be run on demand" on; "If the task fails, restart every 5 min,
  up to 3 times".

`bootstrap.ps1` does the following (idempotent): copy to ProgramData → if the service does not
exist, `tolikopa-agent.exe install` → if the version is old, stop/replace/start → if not running,
start.

## 4) Link the GPO to the target

Drag/link the GPO to the OU that holds the computers (e.g.
`OU=Computers,DC=corp,DC=example,DC=com`) or to the domain root. For specific machines only: keep
just the relevant groups/computers in the GPO's **Security Filtering**.

## 5) Test

On a test PC:
```powershell
gpupdate /force                                   # apply the policy immediately
Get-ScheduledTask -TaskName TolikopaAgent            # was the task created
Start-ScheduledTask -TaskName TolikopaAgent          # trigger it by hand
Get-Service Tolikopa*                                # service Running
Get-Content C:\ProgramData\Tolikopa\logs\agent.log -Tail 20
```
Within a few minutes the machine appears in the panel (`https://10.0.0.10`).

## Updating (self-update)

Copying the new `tolikopa-agent.exe` to NETLOGON is enough. On the next GP refresh/startup,
`bootstrap.ps1` compares the version and updates the service. You do not need to touch the GPO.

## Removal

```powershell
# On the client (or via a removal GPO):
& "C:\ProgramData\Tolikopa\tolikopa-agent.exe" uninstall
Remove-Item -Recurse -Force C:\ProgramData\Tolikopa
Unregister-ScheduledTask -TaskName TolikopaAgent -Confirm:$false
```
Removing/unlinking the GPO stops new installations.

## Antivirus / SmartScreen note

An unsigned LocalSystem exe can produce false positives. Options: sign `tolikopa-agent.exe` with
Authenticode (recommended), or add a Defender exclusion via GPO: `Computer Configuration →
Policies → Administrative Templates → Windows Components → Microsoft Defender Antivirus →
Exclusions` → path `C:\ProgramData\Tolikopa\tolikopa-agent.exe`.

## Troubleshooting (silent install "does nothing")

- **Most common cause:** SYSTEM accesses network shares as the machine account (`CORP\PC$`).
  NETLOGON handles this automatically; if you use a **custom** share, grant Read to "Domain
  Computers".
- Task is not created → is the GPP schema "At least Windows 7"; is the GPO linked to the right OU;
  `gpupdate`.
- Service does not start → `C:\ProgramData\Tolikopa\logs\agent.log`; exe architecture (x64);
  SmartScreen block.
