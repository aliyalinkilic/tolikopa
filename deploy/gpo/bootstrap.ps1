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
