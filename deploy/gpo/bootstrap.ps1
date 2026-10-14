<#
    Tolikopa Agent - GPO bootstrap (idempotent, silent).
    Installs / updates / repairs the "Tolikopa" Windows service from the payload in NETLOGON.
    Run hidden as SYSTEM by the GPP Scheduled Task:
      powershell -WindowStyle Hidden -NonInteractive -ExecutionPolicy Bypass -File <this file>

    Notes:
      * The log is written as ASCII.
      * tolikopa-agent.exe is copied to the local disk first and handled there (avoids SYSTEM stalls on the network).
      * Every step is logged; the output of "tolikopa-agent.exe install" is captured too.
      * The running exe is never overwritten (stop -> replace -> start).
#>

$ErrorActionPreference = 'Stop'
$ServiceName = 'Tolikopa'
$DestDir  = Join-Path $env:ProgramData 'Tolikopa'
$LogDir   = Join-Path $DestDir 'logs'
$SrcDir   = $PSScriptRoot                     # e.g. \\CORP\NETLOGON\Tolikopa
$SrcExe   = Join-Path $SrcDir  'tolikopa-agent.exe'
$SrcCfg   = Join-Path $SrcDir  'config.json'
$DestExe  = Join-Path $DestDir 'tolikopa-agent.exe'
$DestCfg  = Join-Path $DestDir 'config.json'
$StageExe = Join-Path $DestDir 'tolikopa-agent.new.exe'

$SidSystem = New-Object Security.Principal.SecurityIdentifier 'S-1-5-18'
$SidAdmins = New-Object Security.Principal.SecurityIdentifier 'S-1-5-32-544'

function Ensure-Dir($p) { if (-not (Test-Path -LiteralPath $p)) { New-Item -ItemType Directory -Force -Path $p | Out-Null } }
function Is-Link($p) { [bool]((Get-Item -LiteralPath $p -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) }
function Trusted-Owner($p) {
    $o = (Get-Acl -LiteralPath $p).GetOwner([Security.Principal.SecurityIdentifier])
    return (($o -eq $SidSystem) -or ($o -eq $SidAdmins))
}
# Deletes without following junctions: rd removes the junction itself.
function Remove-Tree($p) {
    if (-not (Test-Path -LiteralPath $p)) { return }
    if ((Get-Item -LiteralPath $p -Force).PSIsContainer) { & cmd.exe /c rd /s /q "$p" *> $null }
    else { Remove-Item -LiteralPath $p -Force -ErrorAction SilentlyContinue }
}
# Owner = Administrators; only SYSTEM and Administrators; inheritance off.
# SIDs, not names: group names differ on localized Windows.
function Lock-Dir($p) {
    $acl = New-Object Security.AccessControl.DirectorySecurity
    $acl.SetOwner($SidAdmins)
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($sid in $SidSystem, $SidAdmins) {
        $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($sid, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')))
    }
    [IO.Directory]::SetAccessControl($p, $acl)
}

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)
if (-not $isAdmin) { exit 1 }

# --- Secure the install folder (before anything else; STOP if this fails). ---
# Standard users can create folders under C:\ProgramData. The agent runs from here
# as LocalSystem, so installing into a folder someone else created beforehand would
# hand that user SYSTEM rights. A folder not owned by SYSTEM/Administrators, or one
# that is a junction, is thrown away as a whole and recreated.
try {
    if (Test-Path -LiteralPath $DestDir) {
        if ((Is-Link $DestDir) -or -not (Trusted-Owner $DestDir)) { Remove-Tree $DestDir }
    }
    if (Test-Path -LiteralPath $DestDir) {
        if ((Is-Link $DestDir) -or -not (Trusted-Owner $DestDir)) { exit 1 }
    }
    else { New-Item -ItemType Directory -Force -Path $DestDir | Out-Null }
    Lock-Dir $DestDir
    if ((Is-Link $DestDir) -or -not (Trusted-Owner $DestDir)) { exit 1 }
    # Items left from before the lock that belong to someone else go as well.
    foreach ($item in @(Get-ChildItem -LiteralPath $DestDir -Force)) {
        if ((Is-Link $item.FullName) -or -not (Trusted-Owner $item.FullName)) { Remove-Tree $item.FullName }
    }
}
catch { exit 1 }

Ensure-Dir $LogDir

$LogFile = Join-Path $LogDir 'bootstrap.log'
# ASCII log: reads cleanly with the default Get-Content encoding.
function Log($m) { Add-Content -Path $LogFile -Value ('{0}  {1}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $m) -Encoding Ascii }
function Hash($p) { if (Test-Path $p) { (Get-FileHash -Path $p -Algorithm SHA256).Hash } else { '' } }

try {
    $who = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    Log "bootstrap started. user=$who source=$SrcDir"

    if (-not (Test-Path $SrcExe)) { Log "ERROR: source tolikopa-agent.exe missing: $SrcExe"; exit 1 }
    Log "source tolikopa-agent.exe found"

    # --- Migration: an agent installed under the product's former name, "Gozcu". ---
    # Runs only when the old SERVICE exists: the folder existing is not enough, since
    # any user can create it. The only thing carried over is the machine identity
    # (state\agent_id), so the machine does not show up as a new device in the panel;
    # the unsent queue is not moved. If the old GPO is still in place its task
    # reinstalls the old agent; see docs/UPGRADING.md.
    $LegacyName = 'Gozcu'
    $LegacyDir  = Join-Path $env:ProgramData $LegacyName
    $legacySvc  = Get-Service -Name $LegacyName -ErrorAction SilentlyContinue
    if ($legacySvc) {
        Log "legacy install found - migrating"
        $legacyTrusted = (Test-Path -LiteralPath $LegacyDir) -and -not (Is-Link $LegacyDir) -and (Trusted-Owner $LegacyDir)
        if ($legacySvc.Status -ne 'Stopped') {
            Stop-Service -Name $LegacyName -Force -ErrorAction SilentlyContinue
            try { (Get-Service $LegacyName).WaitForStatus('Stopped', '00:00:30') } catch {}
        }
        & sc.exe delete $LegacyName *> $null
        try { Unregister-ScheduledTask -TaskName ($LegacyName + 'Agent') -Confirm:$false -ErrorAction Stop } catch {}

        if ($legacyTrusted) {
            $oldId = Join-Path $LegacyDir 'state\agent_id'
            $newState = Join-Path $DestDir 'state'
            $newId = Join-Path $newState 'agent_id'
            if ((Test-Path -LiteralPath $oldId -PathType Leaf) -and -not (Test-Path -LiteralPath $newId) `
                    -and -not (Is-Link $oldId) -and (Trusted-Owner $oldId) -and ((Get-Item -LiteralPath $oldId).Length -le 64)) {
                $id = ([IO.File]::ReadAllText($oldId)).Trim()
                if ($id -match '^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$') {
                    Ensure-Dir $newState
                    [IO.File]::WriteAllText($newId, $id)
                    Log "machine identity migrated"
                }
            }
            Remove-Tree $LegacyDir
            Log "legacy install removed"
        }
        else { Log "legacy folder not trusted - left untouched" }
    }

    # Copy tolikopa-agent.exe to the local disk first (hashing over the network as SYSTEM can stall).
    Log "copying tolikopa-agent.exe locally"
    Copy-Item $SrcExe $StageExe -Force
    Log ("copied (" + [math]::Round((Get-Item $StageExe).Length / 1KB) + " KB)")

    # Sync config.json. The agent reads its config only when the service starts, so
    # the service is restarted below for a changed config to take effect.
    $cfgChanged = $false
    if ((Test-Path $SrcCfg) -and ((Hash $SrcCfg) -ne (Hash $DestCfg))) {
        Copy-Item $SrcCfg $DestCfg -Force
        $cfgChanged = $true
        Log "config.json updated"
    }

    $svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    $needUpdate = ((Hash $StageExe) -ne (Hash $DestExe))

    if ($null -eq $svc) {
        Log "service missing - installing"
        Copy-Item $StageExe $DestExe -Force
        $out = (& $DestExe install 2>&1 | Out-String).Trim()
        Log ("install output: " + ($out -replace '\s+', ' '))
        Start-Sleep -Seconds 2
        $svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
        if ($null -eq $svc) { Log "ERROR: service not present after install"; exit 1 }
        Start-Service -Name $ServiceName -ErrorAction SilentlyContinue
        Log "service installed and started"
    }
    elseif ($needUpdate) {
        Log "new build - updating"
        if ($svc.Status -ne 'Stopped') {
            Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
            (Get-Service $ServiceName).WaitForStatus('Stopped', '00:00:30')
        }
        Copy-Item $StageExe $DestExe -Force
        Start-Service -Name $ServiceName -ErrorAction SilentlyContinue
        Log "service updated and started"
    }
    elseif ($cfgChanged) {
        Log "config.json changed - restarting service"
        if ($svc.Status -ne 'Stopped') {
            Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
            (Get-Service $ServiceName).WaitForStatus('Stopped', '00:00:30')
        }
        Start-Service -Name $ServiceName -ErrorAction SilentlyContinue
        Log "service started with the new config"
    }
    else {
        if ($svc.Status -ne 'Running') {
            Start-Service -Name $ServiceName -ErrorAction SilentlyContinue
            Log "service was not running - started"
        } else {
            Log "service up to date and running - nothing to do"
        }
    }

    Remove-Item $StageExe -Force -ErrorAction SilentlyContinue

    try {
        $lines = Get-Content $LogFile -ErrorAction SilentlyContinue
        if ($lines.Count -gt 500) { $lines | Select-Object -Last 300 | Set-Content $LogFile -Encoding Ascii }
    } catch {}

    exit 0
}
catch {
    Log ("ERROR: " + $_.Exception.Message)
    exit 1
}
