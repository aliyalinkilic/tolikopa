# Installation Guide

This guide takes you from an empty Ubuntu server to a domain where every PC reports to the
Tolikopa panel. Follow the steps in order. It is a walkthrough; the reference documents hold the
details:

- [DEPLOYMENT.md](DEPLOYMENT.md): the Ubuntu server (`setup.sh`, `.env`, TLS, backups)
- [GPO-DEPLOYMENT.md](GPO-DEPLOYMENT.md): the Windows rollout (`tolikopa-setup.exe`, the GPO, manual setup)
- [SECURITY.md](SECURITY.md): why things are built the way they are

All names and addresses below are examples. Replace them with your own:

| Example | Meaning |
|---------|---------|
| `corp.local` | your Active Directory domain (FQDN) |
| `CORP` | your domain's NetBIOS name |
| `10.0.0.10` | the Ubuntu server's IP address |
| `tolikopa.corp.local` | the panel's DNS name |
| `OU=Workstations,DC=corp,DC=local` | the OU the GPO is linked to |

---

## 1. What you are installing

Tolikopa has three parts:

1. **The server** (Ubuntu, Docker): the database, the API and the web panel, behind Caddy on
   ports 80/443. You install it with `setup.sh`.
2. **The agent** (`tolikopa-agent.exe`): a small Windows service on every domain PC. It sends
   system-health and usage metrics to the server. You never install it by hand: a Group Policy
   Object (GPO) installs it.
3. **The setup tool** (`tolikopa-setup.exe`): you download it from the panel and run it once on a
   domain-joined admin PC. It copies the agent to `NETLOGON`, adds the panel's DNS record, and
   creates the GPO that installs the agent and makes the PCs trust the panel's certificate.

How they connect: each agent sends its data to the server over HTTPS
(`https://tolikopa.corp.local`) and checks the server's certificate against a copy it holds. The
server never connects to the PCs and **never holds domain credentials**. All domain changes are
made by the setup tool, under your own Windows session, on your own PC. The panel never asks for a
domain password.

---

## 2. Before you start

Collect these values first. Each item explains how to find the value.

### 2.1 Ubuntu server

| Requirement | Detail |
|-------------|--------|
| Operating system | Tested on **Ubuntu 24.04 LTS** |
| Memory | **2 GB** minimum (the image build needs it) |
| Disk | **10 GB or more free** for Docker's data directory (normally `/var/lib/docker`) |
| IP address | **Static IP address** (or a DHCP reservation). The certificate and the agents' configuration contain this address |
| Inbound | TCP **80** and **443** from the LAN (from every PC and from admin browsers) |
| Outbound | Internet access to Docker Hub (`registry-1.docker.io`), `proxy.golang.org` and `registry.npmjs.org`. The build downloads base images, Go modules and npm packages. If you use a proxy, configure it for Docker as well |

To see the server's IP address, run on the server:

```bash
ip -4 addr
hostname -I
```

### 2.2 The panel's DNS name

Choose the name that browsers and agents will use to reach the panel. The setup tool creates the
DNS record later. **Do not create it yourself now.**

- **Recommended:** `tolikopa.<your AD domain>`, for example `tolikopa.corp.local`. The tool adds an
  A record to the existing `corp.local` zone.
- **Also possible:** a separate name such as `tolikopa.local`. No existing zone contains it, so
  the tool creates a new Active Directory-integrated zone with exactly that name.

The name **must not already exist** pointing to another address. The tool never overwrites an
existing record: it stops instead.

The server's certificate is created for this name and the server's IP. After that, the name
cannot be changed without creating a new certificate.

### 2.3 Domain FQDN and NetBIOS name

A domain has two names:

- The **FQDN** (fully qualified domain name) is the DNS name of the domain, for example
  `corp.local`. The setup tool uses it in paths such as `\\corp.local\NETLOGON`.
- The **NetBIOS name** is the short, older name (at most 15 characters), for example `CORP`. You
  see it in `CORP\username`. In the panel it is optional.

**FQDN.** Use one of these on any domain PC:

```powershell
(Get-CimInstance Win32_ComputerSystem).Domain
$env:USERDNSDOMAIN
```

```cmd
systeminfo | findstr /B /C:"Domain"
```

You can also see it in **Active Directory Users and Computers** (ADUC): it is the name at the top
of the tree.

**NetBIOS name.** Use one of these:

```powershell
$env:USERDOMAIN                      # when signed in with a domain account
(Get-ADDomain).NetBIOSName           # needs the RSAT Active Directory module
```

```cmd
nbtstat -n
```

`nbtstat -n` lists the domain name with the type `<00>  GROUP`. In ADUC, right-click the domain →
**Properties** → **General**. The field **Domain name (pre-Windows 2000)** shows the NetBIOS name.

### 2.4 Where to link the GPO

The GPO can apply to:

- the **whole domain** (every computer in it), or
- one **OU** (organisational unit) and everything under it.

**Start with a test OU** that contains one or two PCs. When it works, link the GPO to a wider
target or run the wizard again with the new target.

For an OU you need its **distinguishedName**, for example `OU=Workstations,DC=corp,DC=local`. To
find it, use PowerShell (needs the RSAT Active Directory module):

```powershell
Get-ADOrganizationalUnit -Filter 'Name -eq "Workstations"' | Select-Object DistinguishedName
```

Or use ADUC:

1. **View** → **Advanced Features** (turn it on).
2. Right-click the OU → **Properties** → **Attribute Editor** tab.
3. Select **distinguishedName** → **View** → copy the value.

> **Certificate trust follows the GPO.** The GPO also makes PCs trust the panel's certificate.
> Only PCs under the GPO's target receive this trust. If your own admin PC is in another OU, its
> browser keeps showing a certificate warning. To fix that, see
> [DEPLOYMENT.md → TLS](DEPLOYMENT.md#tls).

### 2.5 The admin PC

You run `tolikopa-setup.exe` on this PC. It needs:

- **Domain membership.** It must be joined to the same domain as the FQDN above.
- **The right account.** Sign in as a **Domain Admin**, or as an account with these delegated
  rights:
  - write access to the `NETLOGON` share;
  - the right to create GPOs (Domain Admins or Group Policy Creator Owners);
  - the right to link GPOs on the target OU (or the domain root);
  - the right to add DNS records and zones (Domain Admins or **DnsAdmins**).

  See [GPO-DEPLOYMENT.md](GPO-DEPLOYMENT.md#recommended-path-tolikopa-setupexe).
- **The RSAT Group Policy and DNS tools.** A domain controller already has both.

On Windows 10/11, install the tools in an **elevated** PowerShell window:

```powershell
Add-WindowsCapability -Online -Name Rsat.GroupPolicy.Management.Tools~~~~0.0.1.0
Add-WindowsCapability -Online -Name Rsat.Dns.Tools~~~~0.0.1.0
```

On Windows Server:

```powershell
Install-WindowsFeature GPMC, RSAT-DNS-Server
```

To check that both are installed:

```powershell
Get-Module -ListAvailable GroupPolicy, DnsServer
```

Both modules must appear in the output. For the optional `Get-ADDomain` /
`Get-ADOrganizationalUnit` commands above, you also need the RSAT AD module
(`Rsat.ActiveDirectory.DS-LDS.Tools~~~~0.0.1.0`).

### 2.6 Checklist

- [ ] Ubuntu server with a static IP: `10.0.0.10`
- [ ] Panel DNS name: `tolikopa.corp.local`
- [ ] Domain FQDN: `corp.local`
- [ ] NetBIOS name: `CORP`
- [ ] GPO target: whole domain or `OU=Workstations,DC=corp,DC=local`
- [ ] Admin PC: domain-joined, Domain Admin (or delegated rights), RSAT Group Policy + DNS tools
- [ ] A test PC under the GPO target

---

## 3. Step 1: Prepare the Ubuntu server

You need **Docker** (Engine 24 or newer, with the Compose and Buildx plugins) and **git**.

`setup.sh` installs nothing itself. It checks everything first. If something is missing, it stops
and prints the exact commands to install it. So you can also skip this step, run `setup.sh`
(Step 3) first, and run the commands it prints.

Install Docker from Docker's official apt repository. Run these in order (they are the same
commands `setup.sh` prints):

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
```

Install git:

```bash
sudo apt-get install -y git
```

`setup.sh` also needs `curl`, `openssl`, `iproute2` and `systemd`. A standard Ubuntu server has
them already.

---

## 4. Step 2: Get the code

On the server, as your normal user (not root):

```bash
git clone https://github.com/aliyalinkilic/tolikopa.git ~/tolikopa
```

**Private fork:** GitHub asks for a username and password. Enter your GitHub username and a
**personal access token** instead of your password; GitHub does not accept account passwords for
git. Create a **fine-grained** token that has access to this repository only, with
**Contents: Read-only** permission.

---

## 5. Step 3: Run setup.sh

```bash
cd ~/tolikopa && sudo ./setup.sh
```

The first question is the language (`1) Türkçe`, `2) English`; the default follows the server's
locale). To skip it, pass `--lang en` or `--lang tr`. Without a terminal (unattended runs) the
script uses `TOLIKOPA_LANG` or `--lang`, otherwise Turkish.

### 5.1 Prerequisite check

The script first prints `Checking prerequisites...`. If something is missing, it prints
`Cannot continue — missing:` and a list, then `To fix (run in order):` and the commands. Run them
and start the script again. If everything is in place:

```
✓ Prerequisites OK (Docker 28.x.x, Compose 2.x.x, Buildx 0.x.x).
```

It checks Docker Engine ≥ 24.0.0, Compose ≥ 2.20.0, Buildx ≥ 0.17.0, free ports 80/443, ≥ 10 GB
free disk, and outbound access to the three download sites. With less than about 2 GB of memory it
only warns. The full table is in [DEPLOYMENT.md](DEPLOYMENT.md#prerequisites).

### 5.2 The questions

The questions are asked only on the first run.

**1. Panel DNS name.** The script prints:

```
The panel's address. PCs and browsers reach the server by this name; tolikopa-setup.exe later adds it to the domain's DNS.
If your domain is, say, corp.local, tolikopa.corp.local is a good choice.
  Panel DNS name: tolikopa.corp.local
```

Enter the name you chose in 2.2. Rules:

- It must be a full name with at least two parts, such as `tolikopa.corp.local`.
- Only letters, digits and hyphens are allowed in each part.
- The last part must not be a number. An IP address is not accepted here.
- Upper case is converted to lower case.

If the name is not valid, the script prints `Enter a full name such as tolikopa.corp.local.` and
asks again.

**2. This server's IP address.**

```
  This server's IP address [10.0.0.10]:
```

The value in brackets is detected when the script runs, from the server's default route. Press
**Enter** to accept it, or type another address. It must be an IPv4 address (not `0.x`, `127.x`
or `255.255.255.255`). If the address is not configured on any interface of the server, the
script warns, but continues. This is correct behind NAT.

**3. First administrator account.**

```
First administrator account (you sign in to the panel with it):
  Username [admin]:
  Password (at least 10 characters):
  Password (again):
```

- **Username:** letters, digits and `. _ - @`, at most 64 characters. Enter accepts `admin`.
- **Password:** at least 10 characters and at most 72 bytes. It is not shown while you type. It
  is never written to `.env`, a command line or a log.
- **Unattended:** the answers can come from `TOLIKOPA_HOST`, `TOLIKOPA_IP`,
  `TOLIKOPA_ADMIN_USER` and, for the password, preferably `TOLIKOPA_ADMIN_PASSWORD_FILE` (a file
  owned by root, mode 600 or 400, the password on its first line). See
  [DEPLOYMENT.md → Unattended installation](DEPLOYMENT.md#unattended-installation).

**4. Summary and confirmation.**

```
Summary:
  Panel:     https://tolikopa.corp.local   (https://10.0.0.10)
  Admin:  admin
  Continue? [Y/n]:
```

Press **Enter** (or `y`) to continue. `n` cancels, and nothing is changed.

### 5.3 What the script does next

| Output | What happens |
|--------|--------------|
| `Creating a self-signed certificate: ...` | Creates `deploy/tls/tolikopa.crt` and `tolikopa.key`, valid for 3650 days, for exactly the panel name and the IP. A certificate left by an interrupted earlier run is reused instead (`Using the existing certificate`) |
| `Generating secrets (deploy/.env)...` | Writes `deploy/.env` (mode 600) with a random database password, ingest token and session key, plus the panel name and IP. It comes after the certificate on purpose: if the script stops before this point, the next run is a first run again |
| `ufw: 80 and 443 allowed.` | Only if `ufw` is already enabled. Otherwise the firewall is not changed. ufw does not filter ports that Docker publishes; see [DEPLOYMENT.md → Firewall](DEPLOYMENT.md#firewall-and-published-ports) |
| `Building images and starting services (5–15 minutes the first time)...` | Builds and starts the database, the API and the web server (Caddy) with Docker Compose |
| `Waiting for the panel to answer...` | Calls the panel over HTTPS on the server itself and checks the certificate. Waits at most 5 minutes |
| `Creating the administrator account: admin` | Creates the account from 5.2 |
| `Installing the Linux agent (the server monitors itself)...` | Installs `/opt/tolikopa/tolikopa-agent` as the systemd service `tolikopa-agent` (sandboxed: read-only file system except `/var/lib/tolikopa`). The server then appears in the panel too |

**Why a self-signed certificate.** The certificate is **not a CA** (`CA:FALSE`). The setup tool
later makes every domain PC trust it, so browsers open the panel without a warning. A CA in that
place would let anyone who steals the key create trusted certificates for any website. This
certificate is valid only for the panel's own name and IP. Details:
[SECURITY.md → TLS](SECURITY.md#tls).

### 5.4 The final output

Example (shortened):

```
────────────────────────────────────────────────────────────
✓ Setup complete.

  Panel:      https://tolikopa.corp.local
              https://10.0.0.10  (use this until the DNS record exists)
  Certificate:  SHA-256 4B:1E:9A:...:C7:02
              valid until: Oct 11 09:30:12 2036 GMT

Next steps:
  1. Open the panel. The browser warns about the certificate: continue if the fingerprint matches the one above.
  2. In the setup wizard enter the domain details and download tolikopa-setup.exe.
  3. Run it on a domain-joined admin PC: it deploys the agent, creates the tolikopa.corp.local DNS record and makes the PCs trust this certificate through the GPO.

  Update:  cd /home/admin/tolikopa && git pull && sudo ./setup.sh
  Back up deploy/.env and deploy/tls/: if the certificate is lost it must be distributed to the PCs again.
```

**Note the SHA-256 fingerprint.** You need it in the next step. To show it again later:

```bash
openssl x509 -in ~/tolikopa/deploy/tls/tolikopa.crt -noout -fingerprint -sha256
```

---

## 6. Step 4: First sign-in

1. On your admin PC, open **`https://10.0.0.10`** (the server's IP). Do not use the DNS name yet:
   the record does not exist until Step 6.
2. The browser shows a certificate warning ("Your connection is not private"). This is expected:
   no PC trusts the certificate yet. Compare the fingerprint before you continue. In Chrome or
   Edge:
   1. Click the **Not secure** badge in the address bar.
   2. Click the certificate entry (for example **Certificate details**).
   3. Find the **SHA-256 fingerprint** of the certificate.
   4. Compare it with the value `setup.sh` printed. The browser may show it with spaces instead of
      colons, or in lower case. The characters must be the same.
3. If it matches, continue: **Advanced** → **Continue to 10.0.0.10 (unsafe)**. If it does not
   match, stop: you are not connected to your server.
4. Sign in with the administrator account from Step 3.
5. The **First-Run Setup** wizard opens at the **Server Address** step. The welcome and account
   steps are already done, because `setup.sh` created the account.

If you close the wizard with **Finish Later**, a banner offers **Go to Setup**. The same settings
are also on the **Setup** page.

---

## 7. Step 5: The setup wizard

### 7.1 Server Address

This is the address the agent on every PC will call. It must be reachable from every PC, and it
must stay the same.

- Under **Detected Addresses**, select **`https://tolikopa.corp.local`**. The name does not
  resolve yet; that is fine. The setup tool creates the DNS record in Step 6.
- Prefer the **name** over the IP. If the IP changes later, every agent's configuration breaks.
- **Another Address** lets you type an address yourself. It must look like
  `https://server-name[:port]`.

Click **Save and Continue**.

### 7.2 Domain

| Field | What to enter |
|-------|---------------|
| **Domain (FQDN)** | `corp.local` (from 2.3). It must match the domain of the admin PC exactly, or the setup tool refuses to run |
| **NetBIOS Name** | `CORP` (optional, at most 15 characters) |
| **Where to Link the GPO** | **Whole Domain**, or **A Specific OU** |
| **The OU's distinguishedName** | Only for **A Specific OU**, for example `OU=Workstations,DC=corp,DC=local`. It must end with the domain part (`DC=corp,DC=local`); the form shows the expected ending |
| **GPO Name** | Default `Tolikopa Agent`. It must not be the name of an existing GPO that the tool did not create |
| **Add a Microsoft Defender Exclusion** | **Recommended.** The agent is not code-signed, so Defender may flag it. The exclusion covers only the agent binary (`%ProgramData%\Tolikopa\tolikopa-agent.exe` and its update copy), not the folder. Other antivirus products need their own exclusion |

Click **Save and Continue**. The **Required Rights** panel next to the form repeats the account
requirements from 2.5.

### 7.3 Download and Run

1. Check the **Readiness Check** list. All four items must show **Ready**:
   - **Server address**
   - **TLS certificate**
   - **Agent files**
   - **Domain details**

   If an item shows **Missing**, the list explains the fix. Click **Refresh** after fixing it.
2. Under **Agent Build**, note the **SHA-256** of the agent. The setup tool prints the same hash
   in Step 6; compare the two.
3. Click **Download tolikopa-setup.exe**.

`tolikopa-setup.exe` carries your configuration, the agent files and the **ingest token**. Treat
the file as sensitive: anyone who has it can send fake reports to your server. Do not e-mail it
or leave it on a shared drive. Delete it when you are done; you can download it again any time.

The console language of the tool follows the panel's language at the moment you download it.

Click **Next** to open the last step, **First PC**. Leave this page open; you come back to it in
Step 7.

---

## 8. Step 6: Run tolikopa-setup.exe on the admin PC

### 8.1 Start the tool

1. Copy `tolikopa-setup.exe` to the admin PC from 2.5.
2. **SmartScreen.** The file is not signed, so Windows may show "Windows protected your PC". Click
   **More info** → **Run anyway**.
3. **Account.** The tool works with the rights of the Windows account that runs it. If you are
   not signed in as a domain admin, Shift + right-click the file → **Run as different user**, and
   enter the domain admin account.

### 8.2 Dry run first

Open PowerShell in the folder that contains the file and run:

```powershell
.\tolikopa-setup.exe -dry-run
```

The tool prints `DRY RUN: nothing will be changed.`, checks the PC and the domain, and prints its
plan. In a dry run it does not ask for the DNS name; it uses the defaults from the certificate.
If a check fails, the error is printed above the plan. Fix it (see
[Troubleshooting](#13-troubleshooting)) before the real run.

### 8.3 The real run

Run the tool without `-dry-run`, from the console (`.\tolikopa-setup.exe`) or with a
double-click. With a double-click the window stays open at the end (`Press Enter to close.`).

**1. DNS name and IP address.**

```
Tolikopa Setup — deploy the agent to your domain
================================================================
The panel's address in your DNS. The tool creates this record so that browsers and agents find the server by name.
Press Enter to accept the value in brackets, or type another. Type - to skip this step.
  DNS name [tolikopa.corp.local]:
  IP address [10.0.0.10]:
```

- The defaults come from the server's certificate. Press **Enter** to keep each one.
- The DNS name must be a name the certificate covers. If you type another name, the tool prints
  `The panel's certificate is valid only for: ...` and asks again.
- Type `-` at the name prompt to skip the DNS step. Then create the record yourself.

**2. Checks.** The tool prints what it found:

```
Checking this computer and the domain…
  Computer:        ADMIN-PC01
  Signed in as:    CORP\itadmin
  Server:          https://tolikopa.corp.local
  Agent version:   ...
  SHA-256:         <hash of tolikopa-agent.exe>
  Certificate:     tolikopa.corp.local, 10.0.0.10
  Fingerprint:     <certificate fingerprint>
  Valid until:     2036-10-11
```

Compare **SHA-256** with the value the panel showed in 7.3. If they differ, stop.

**3. The plan.** Read it before you confirm:

```
What will happen:
  1. Copy tolikopa-agent.exe, bootstrap.ps1, config.json to \\corp.local\NETLOGON\Tolikopa
  2. Add the DNS record tolikopa.corp.local → 10.0.0.10 to the zone corp.local
  3. Create or update the GPO "Tolikopa Agent" (computer policy: scheduled task "TolikopaAgent", runs as SYSTEM at startup and daily)
  4. Make the PCs the GPO applies to trust the panel's certificate (tolikopa.corp.local) — only that certificate, which can vouch for nothing but these names
  5. Add a Microsoft Defender exclusion for %ProgramData%\Tolikopa\tolikopa-agent.exe to the same GPO (the agent binary and its update copy only, not the folder)
  6. Link the GPO to OU=Workstations,DC=corp,DC=local
```

Variations:

- If no zone contains the name (for example `tolikopa.local`), item 2 reads `Create the DNS zone
  tolikopa.local (Active Directory-integrated, replicated to the domain's DNS servers) pointing to
  10.0.0.10`.
- If the record already exists with the same address, it reads `DNS record ... is already in
  place`.
- The Defender item appears only if you selected the exclusion in the wizard.

**4. Endpoint clean-up questions.**

```
Endpoint clean-up (optional). The agent can remove software from a fixed, built-in list and disable listed startup entries.
It does so only on PCs whose local configuration allows it. These answers are stored in config.json on the share, not on the server.
  Allow removal of listed bloatware [y/n, Enter = n]:
  Allow disabling listed startup entries [y/n, Enter = n]:
```

| Question | Meaning |
|----------|---------|
| **Allow removal of listed bloatware** | The agent may uninstall software that is on a fixed list compiled into the agent |
| **Allow disabling listed startup entries** | The agent may disable listed programs that start with Windows |
| **Really act (No = report only what would be done)** | Asked only if you answered `y` above. `n` means the agent only reports what it would do |

All three are **off by default**. Press **Enter** to keep them off. You can turn them on later by
running the tool again. The answers are stored only in `config.json` on the `NETLOGON` share; the
server cannot change them. Even when they are on, the panel starts in "Dry Run" mode, and nothing
is removed until you choose otherwise there. Details:
[GPO-DEPLOYMENT.md → Endpoint hygiene switches](GPO-DEPLOYMENT.md#endpoint-hygiene-switches-optional-off-by-default)
and [AGENT.md](AGENT.md).

**5. Confirm.**

```
Continue? Type YES and press Enter:
```

Type **`YES`** (in a Turkish console: **`EVET`**). Any other answer cancels, and nothing is
changed.

**6. Success.**

```
Publishing files to NETLOGON…
  config.json        updated
  bootstrap.ps1      updated
  tolikopa-agent.exe updated
Creating the panel's DNS record…
  tolikopa.corp.local → 10.0.0.10 added to corp.local
Creating and linking the GPO…
  GPO created (<GPO id>)
  panel certificate trusted through the GPO
  linked to OU=Workstations,DC=corp,DC=local

Done. PCs install the agent at their next start or policy refresh (up to ~2 hours).
To test now, on one PC:  gpupdate /force  and then  Start-ScheduledTask TolikopaAgent
The machine appears in the panel within a few minutes.
```

It is safe to run the tool again. It updates the existing files, record and GPO, and creates no
duplicates.

### 8.4 Flags

| Flag | Meaning |
|------|---------|
| `-dry-run` | Show the checks and the plan; change nothing |
| `-yes` | Ask nothing: use the defaults (or the `-dns-*` flags) and skip the confirmation. The clean-up switches keep their current values |
| `-dns-name <name>` | The panel's DNS name (default: the name in the certificate) |
| `-dns-ip <ip>` | The address the DNS name points to (default: the address in the certificate) |
| `-skip-dns` | Do not create or check the DNS record |
| `-lang en` / `-lang tr` | Console language; overrides the panel language |
| `-remove` | Unlink and delete the GPO (and the certificate trust) and delete the `NETLOGON\Tolikopa` folder. See [Uninstall](#14-uninstall) |
| `-version` | Print the tool version |

Example:

```powershell
.\tolikopa-setup.exe -lang en -dns-name tolikopa.corp.local -dns-ip 10.0.0.10
```

---

## 9. Step 7: Verify on one PC

Use a test PC under the GPO target.

1. Open an **elevated** PowerShell window (Run as administrator).
2. Apply the policy and run the task now:

   ```powershell
   gpupdate /force
   Start-ScheduledTask -TaskName TolikopaAgent
   ```

3. Wait about a minute. Check the service:

   ```powershell
   Get-Service Tolikopa
   ```

   `Status` must be `Running`.
4. Check the installer log:

   ```powershell
   Get-Content C:\ProgramData\Tolikopa\logs\bootstrap.log -Tail 20
   ```

   A first installation looks like this:

   ```
   2026-10-14 10:02:11  bootstrap started. user=NT AUTHORITY\SYSTEM source=\\corp.local\NETLOGON\Tolikopa
   2026-10-14 10:02:11  source tolikopa-agent.exe found
   2026-10-14 10:02:11  copying tolikopa-agent.exe locally
   2026-10-14 10:02:12  copied (9876 KB)
   2026-10-14 10:02:12  config.json updated
   2026-10-14 10:02:12  service missing - installing
   2026-10-14 10:02:13  install output: Tolikopa service installed.
   2026-10-14 10:02:15  service installed and started
   ```

   On later runs the last line is `service up to date and running - nothing to do`. A line that
   starts with `ERROR:` shows what went wrong.
5. The agent's own log is `C:\ProgramData\Tolikopa\logs\agent.log`:

   ```powershell
   Get-Content C:\ProgramData\Tolikopa\logs\agent.log -Tail 20
   ```

6. Check the DNS record:

   ```powershell
   Resolve-DnsName tolikopa.corp.local
   ```

   It must return `10.0.0.10`.
7. In a browser, open **`https://tolikopa.corp.local`**. There is no certificate warning once the
   policy has applied. To get the same result on your own admin PC, run `gpupdate /force` there
   too (it must be under the GPO target; see 2.4).

Within a few minutes the PC appears in the panel. On the wizard's **First PC** step, the status
turns green with `1 PC connected`. Click **Finish Setup**.

---

## 10. Step 8: The rest of the fleet

There is nothing to do per PC.

- Every PC under the GPO target receives the policy at its next Group Policy refresh (about every
  90 minutes, plus a random offset of up to 30 minutes) or at restart. The policy creates the
  `TolikopaAgent` scheduled task.
- The task installs the agent. It runs as SYSTEM **1 minute after Group Policy registers it**
  (so a policy refresh alone is enough), **1 minute after startup** and **daily at 08:00**.
- The task also repairs and updates the agent on every run.

The **Setup** page shows the fleet by agent version. To extend the rollout from a test OU, run
the wizard again (Setup page → **Run the Wizard Again**), change **Where to Link the GPO**,
download the tool again and run it.

---

## 11. Updating

On the server:

```bash
cd ~/tolikopa && git pull && sudo ./setup.sh
```

When `deploy/.env` exists, `setup.sh` asks no questions. It checks the prerequisites, rebuilds the
images, restarts the services and refreshes the Linux agent. Database migrations run
automatically.

If the update contains a new agent build, the **Setup** page shows **Agent Update** with
**Download tolikopa-update.exe**. Run that tool on a domain-joined PC with an account that can
write to `NETLOGON`. It only replaces the files on `NETLOGON`; the GPO, DNS and certificate are
not touched. Each PC updates itself at its next start or daily run.

After an update, reload the panel with a hard refresh (Ctrl+F5) so the browser loads the new
version.

---

## 12. Back up

Back up three things and keep the copies off the server:

1. **`deploy/.env`**: the secrets, including the ingest token the agents use.
2. **`deploy/tls/`**: the certificate and its key. If you lose them, you need a new certificate.
   Then every PC's trust must be redistributed (run `tolikopa-setup.exe` again) and every agent's
   pinned certificate must be updated.
3. **The database**:

   ```bash
   sudo docker compose -f ~/tolikopa/deploy/docker-compose.yml exec -T db pg_dump -U tolikopa -d tolikopa -Fc > tolikopa-$(date +%F).dump
   ```

   `-T` is required; without it the binary dump is corrupted.

`deploy/.env` and `deploy/tls/tolikopa.key` belong to root (mode 600). Use `sudo` to copy them,
for example:

```bash
sudo tar czf tolikopa-config-$(date +%F).tgz -C ~/tolikopa/deploy .env tls
```

The archive contains secrets: store it securely. Restore notes are in
[DEPLOYMENT.md → Backup](DEPLOYMENT.md#backup).

---

## 13. Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `setup.sh` stops with `Cannot continue — missing:` | A prerequisite is missing | Run the commands under `To fix (run in order):` in order, then `sudo ./setup.sh` again |
| `port 80 is in use (...)` / `port 443 is in use (...)` | Another web server or container holds the port | The script names the process and suggests a command, for example `sudo systemctl disable --now apache2`. Stop it or move it to another port |
| `cannot reach https://... (internet/proxy)` | No outbound internet, or a proxy | Allow outbound access. With a proxy, configure Docker too: https://docs.docker.com/engine/daemon/proxy/ |
| `The panel did not answer within 5 minutes.` | A container failed to start | `sudo docker compose -f ~/tolikopa/deploy/docker-compose.yml logs api web` |
| `No certificate in deploy/tls.` | `deploy/.env` exists but the certificate is gone | Restore `deploy/tls/` from a backup. See [DEPLOYMENT.md → Troubleshooting](DEPLOYMENT.md#troubleshooting) |
| The panel does not open from PCs | Firewall, or `BIND_ADDR` | Allow TCP 443 (and 80) to the server in any network firewall (ufw does not affect Docker-published ports). If `BIND_ADDR` is set in `deploy/.env`, the panel answers only on that address |
| The browser still warns about the certificate | The GPO has not applied, or the PC is outside the GPO target | On the PC: `gpupdate /force`, then restart the browser. Check that the PC is in the target OU. Open `certlm.msc` → **Trusted Root Certification Authorities** → **Certificates**: a certificate issued to `tolikopa.corp.local` must be there. Open the panel by its **name**: the IP is covered too, but other names are not. Firefox may use its own certificate store |
| `tolikopa.corp.local` does not resolve | DNS cache, or no record | `ipconfig /flushdns`, then `Resolve-DnsName tolikopa.corp.local`. In **DNS Manager** (`dnsmgmt.msc`), check that the A record (or the `tolikopa.local` zone) exists. If you used `-skip-dns` or `-`, create it yourself |
| Setup tool: `The DNS name ... already points elsewhere (...)` | A record with that name points to another address, or a CNAME/AAAA exists | Delete the old record, or choose another name the certificate covers, or run with `-skip-dns` and create the record yourself |
| Setup tool: `The Group Policy PowerShell module is missing.` | RSAT Group Policy tools are not installed | Elevated PowerShell: `Add-WindowsCapability -Online -Name Rsat.GroupPolicy.Management.Tools~~~~0.0.1.0` (Windows Server: `Install-WindowsFeature GPMC`). Or run the tool on a domain controller |
| Setup tool: `The DNS Server PowerShell module is missing.` | RSAT DNS tools are not installed | `Add-WindowsCapability -Online -Name Rsat.Dns.Tools~~~~0.0.1.0` (Windows Server: `Install-WindowsFeature RSAT-DNS-Server`). Or run with `-skip-dns` |
| Setup tool: `This computer is joined to ..., but the bundle was prepared for ...` | The **Domain (FQDN)** in the wizard is wrong | Fix it on the Domain step, save, download the tool again |
| Setup tool: `A GPO named "..." already exists and was not created by this tool.` | Name clash with an existing GPO | Choose another **GPO Name**, save, download the tool again |
| Setup tool: `The domain refused the change.` | The account lacks a right | Check the rights in 2.5, or run as a domain admin (Shift + right-click → **Run as different user**) |
| A PC does not appear in the panel | The agent is not installed, or cannot reach the server | Read `C:\ProgramData\Tolikopa\logs\bootstrap.log` and `agent.log`. Check `Get-ScheduledTask TolikopaAgent` (no task: the GPO has not applied; run `gpupdate /force` and check the target OU). Check that the PC reaches the server: `Test-NetConnection tolikopa.corp.local -Port 443`. The server accepts agent data only from private addresses (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`); a PC that reaches it from a public address is rejected |
| A wizard step's **Save** seems to do nothing | The browser runs an old copy of the panel after an update | Hard-refresh the page (Ctrl+F5, or Ctrl+Shift+R) |

More server-side checks are in [DEPLOYMENT.md → Verification](DEPLOYMENT.md#verification). More
PC-side checks are in [GPO-DEPLOYMENT.md](GPO-DEPLOYMENT.md#troubleshooting-silent-install-does-nothing).

---

## 14. Uninstall

### 14.1 Domain side

On the admin PC, run the same `tolikopa-setup.exe` (not `tolikopa-update.exe`):

```powershell
.\tolikopa-setup.exe -remove
```

It shows its plan, asks for `YES`, then unlinks and deletes the GPO (which removes the certificate
trust too) and deletes `\\corp.local\NETLOGON\Tolikopa`.

It does **not** remove:

- **The DNS record.** Delete it in DNS Manager, or with PowerShell:

  ```powershell
  # A record in an existing zone:
  Remove-DnsServerResourceRecord -ComputerName <DC name> -ZoneName corp.local -Name tolikopa -RRType A -Force
  # Or, if the tool created its own zone (for example tolikopa.local):
  Remove-DnsServerZone -ComputerName <DC name> -Name tolikopa.local -Force
  ```

- **Agents already installed on the PCs**, and their `TolikopaAgent` scheduled task. On each PC,
  in an elevated PowerShell window (or through a removal script/GPO):

  ```powershell
  & "C:\ProgramData\Tolikopa\tolikopa-agent.exe" uninstall
  Remove-Item -Recurse -Force C:\ProgramData\Tolikopa
  Unregister-ScheduledTask -TaskName TolikopaAgent -Confirm:$false
  ```

### 14.2 Server side

```bash
# Stop and delete the containers and ALL data (database volume included):
sudo docker compose -f ~/tolikopa/deploy/docker-compose.yml down -v

# Remove the server's own Linux agent:
sudo systemctl disable --now tolikopa-agent
sudo rm -f /etc/systemd/system/tolikopa-agent.service
sudo systemctl daemon-reload
sudo rm -rf /opt/tolikopa /etc/tolikopa /var/lib/tolikopa

# Finally, the code, secrets and certificate:
sudo rm -rf ~/tolikopa
```

`down -v` deletes the database permanently. Take a backup first (section 12) if you may need the
data.
