<#
    Tolikopa setup - domain side. Run by tolikopa-setup.exe as the signed-in admin.
    Every value arrives as a parameter that the exe has already validated; this
    script never builds a command line from them.

    Actions:
      preflight  read-only checks, prints what it found
      protect    restrict the NETLOGON folder to computer accounts and admins
      dns        make the panel's name resolve: an A record in the zone that holds
                 it, or a new AD-integrated zone of that name; never overwrites
      apply      create (or reuse) the GPO, publish the scheduled task, trust the
                 panel's certificate, link it
      remove     unlink and delete the GPO

    The last line of output is "##RESULT##" + one JSON object.

    Nothing here contacts the network until the local computer's own domain has
    been compared with the requested one: the domain name comes from a download,
    and opening \\<that name>\... would hand the admin's credentials to whoever
    that name points at.
#>
param(
    [Parameter(Mandatory = $true)][ValidateSet('preflight', 'protect', 'dns', 'apply', 'remove')][string]$Action,
    [Parameter(Mandatory = $true)][string]$DomainFqdn,
    [string]$GpoName = '',
    [string]$TargetDn = '',
    [string]$TaskXml = '',
    [string]$SharePath = '',
    # 'on' adds a Microsoft Defender path exclusion for the installed agent binary to
    # the same GPO; 'off' removes it again.
    [ValidateSet('on', 'off')][string]$DefenderExclusion = 'off',
    # The panel's DNS name and address (preflight, dns).
    [string]$DnsName = '',
    [string]$DnsIp = '',
    # DER file of the panel certificate to trust, and its SHA-1 thumbprint (apply).
    # The exe has already checked it is a self-signed, non-CA server certificate.
    [string]$TrustCert = '',
    [string]$TrustThumb = ''
)

$ErrorActionPreference = 'Stop'
$result = [ordered]@{ ok = $false; action = $Action }

# Ownership marker. A GPO is created with this description and is only ever
# modified or deleted while it still carries it; a GPO that merely has the same
# name is left alone.
$Marker = 'Tolikopa agent deployment (managed by tolikopa-setup)'

# Client-side extension registration for "Preferences > Scheduled Tasks" (machine side).
# Without it the XML sits in SYSVOL and no client ever processes it.
$TaskCse = '[{00000000-0000-0000-0000-000000000000}{CAB54552-DEEA-4691-817E-ED4A4D1AFC72}][{AADCED64-746C-4633-A97C-D61349046527}{CAB54552-DEEA-4691-817E-ED4A4D1AFC72}]'

# The same list with the Registry (Administrative Templates) extension in it, which
# carries the Defender exclusion and the trusted panel certificate. Extensions are
# ordered by GUID.
$RegistryCse = '[{35378EAC-683F-11D2-A89A-00C04FBBCFA2}{D02B1F72-3407-48AE-BA88-E8213C6761F1}]'
$FullCse = '[{00000000-0000-0000-0000-000000000000}{CAB54552-DEEA-4691-817E-ED4A4D1AFC72}]' + $RegistryCse + '[{AADCED64-746C-4633-A97C-D61349046527}{CAB54552-DEEA-4691-817E-ED4A4D1AFC72}]'

# Only the two files bootstrap.ps1 writes are excluded, never the folder: a folder
# exclusion would be a place where anything could hide from the scanner.
$DefenderKey = 'HKLM\Software\Policies\Microsoft\Windows Defender\Exclusions'
$DefenderPaths = @('%ProgramData%\Tolikopa\tolikopa-agent.exe', '%ProgramData%\Tolikopa\tolikopa-agent.new.exe')

# Group Policy's part of the computer's trusted root store. CryptoAPI reads the
# certificates under this key as the "Group Policy" physical store of
# LocalMachine\Root; removing the GPO removes the trust again.
$RootKey = 'HKLM\Software\Policies\Microsoft\SystemCertificates\Root\Certificates'

function Get-Pdc {
    $pdc = [string][System.DirectoryServices.ActiveDirectory.Domain]::GetCurrentDomain().PdcRoleOwner.Name
    if (-not $pdc) { throw 'no_domain_controller' }
    return $pdc
}

# Where $DnsName lives: the longest primary forward zone that contains it, the
# relative record name, and the A addresses it has now.
function Find-DnsTarget($server) {
    $name = $DnsName.ToLower()
    $zone = ''
    foreach ($z in @(Get-DnsServerZone -ComputerName $server)) {
        if ($z.IsReverseLookupZone -or $z.IsAutoCreated -or ([string]$z.ZoneType -ne 'Primary')) { continue }
        $zn = ([string]$z.ZoneName).TrimEnd('.').ToLower()
        if (($name -eq $zn -or $name.EndsWith('.' + $zn)) -and $zn.Length -gt $zone.Length) { $zone = $zn }
    }
    $t = @{ zone = $zone; rel = ''; a = @(); other = $false }
    if (-not $zone) { return $t }
    if ($name -eq $zone) { $t.rel = '@' } else { $t.rel = $name.Substring(0, $name.Length - $zone.Length - 1) }
    $recs = @(Get-DnsServerResourceRecord -ComputerName $server -ZoneName $zone -Name $t.rel -ErrorAction SilentlyContinue)
    foreach ($r in $recs) {
        # -Name '@' returns the whole zone; keep only the node that was asked for.
        if ([string]$r.HostName -ne $t.rel) { continue }
        if ($r.RecordType -eq 'A') { $t.a += $r.RecordData.IPv4Address.IPAddressToString }
        elseif (@('CNAME', 'AAAA') -contains [string]$r.RecordType) { $t.other = $true }
    }
    return $t
}

function Get-DnsState($t) {
    if (-not $t.zone) { return 'new_zone' }
    if ($t.other) { return 'conflict' }
    if ($t.a.Count -eq 0) { return 'add' }
    if (($t.a.Count -eq 1) -and ($t.a[0] -eq $DnsIp)) { return 'present' }
    return 'conflict'
}

function Finish {
    Write-Output ('##RESULT##' + ($result | ConvertTo-Json -Compress -Depth 4))
    exit 0
}

try {
    # ---- local facts only ----
    $cs = Get-CimInstance -ClassName Win32_ComputerSystem
    $result.computer        = $cs.Name
    $result.part_of_domain  = [bool]$cs.PartOfDomain
    $result.computer_domain = [string]$cs.Domain
    $result.user            = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $result.gp_module       = [bool](Get-Module -ListAvailable -Name GroupPolicy)
    $domainMatch = $result.part_of_domain -and ($result.computer_domain -ieq $DomainFqdn)
    $result.domain_match = [bool]$domainMatch

    if ($Action -eq 'preflight') {
        $result.share_parent_reachable = $false
        $result.share_exists = $false
        $result.gpo_exists = $false
        $result.gpo_foreign = $false
        $result.dns_module = [bool](Get-Module -ListAvailable -Name DnsServer)
        $result.dns_state = 'unknown'
        if ($domainMatch -and $DnsName -and $result.dns_module) {
            try {
                Import-Module DnsServer
                $t = Find-DnsTarget (Get-Pdc)
                $result.dns_zone = $t.zone
                $result.dns_existing = @($t.a)
                $result.dns_state = Get-DnsState $t
            }
            catch { $result.dns_error = [string]$_.Exception.Message }
        }
        if ($domainMatch) {
            if ($SharePath) {
                $result.share_parent_reachable = [bool](Test-Path -LiteralPath (Split-Path -Parent $SharePath))
                $result.share_exists = [bool](Test-Path -LiteralPath $SharePath)
            }
            if ($result.gp_module -and $GpoName) {
                Import-Module GroupPolicy
                $g = Get-GPO -Name $GpoName -Domain $DomainFqdn -ErrorAction SilentlyContinue
                if ($g) {
                    $result.gpo_exists = $true
                    $result.gpo_id = $g.Id.ToString()
                    $result.gpo_foreign = ($g.Description -ne $Marker)
                }
            }
        }
        $result.ok = $true
        Finish
    }

    if (-not $result.part_of_domain) { throw 'not_domain_joined' }
    if (-not $domainMatch) { throw 'wrong_domain' }

    if ($Action -eq 'protect') {
        # config.json holds the agents' ingest token and NETLOGON is readable by every
        # authenticated user. Only computer accounts (the agents run as SYSTEM, which
        # is the computer account on the network) and admins need this folder.
        if (-not (Test-Path -LiteralPath $SharePath)) { throw 'share_missing' }
        $me = [Security.Principal.WindowsIdentity]::GetCurrent().User
        $domainSid = $me.AccountDomainSid
        if (-not $domainSid) { throw 'not_a_domain_account' }
        $sid = { param($t) New-Object Security.Principal.SecurityIdentifier($t, $domainSid) }
        $full = [Security.AccessControl.FileSystemRights]::FullControl
        $read = [Security.AccessControl.FileSystemRights]::ReadAndExecute
        $grants = @(
            @((& $sid ([Security.Principal.WellKnownSidType]::LocalSystemSid)), $full),
            @((& $sid ([Security.Principal.WellKnownSidType]::BuiltinAdministratorsSid)), $full),
            @((& $sid ([Security.Principal.WellKnownSidType]::AccountDomainAdminsSid)), $full),
            @($me, $full),
            @((& $sid ([Security.Principal.WellKnownSidType]::AccountComputersSid)), $read),
            @((& $sid ([Security.Principal.WellKnownSidType]::AccountControllersSid)), $read)
        )
        $acl = New-Object Security.AccessControl.DirectorySecurity
        $acl.SetAccessRuleProtection($true, $false)
        foreach ($g in $grants) {
            $rule = New-Object Security.AccessControl.FileSystemAccessRule($g[0], $g[1], 'ContainerInherit,ObjectInherit', 'None', 'Allow')
            $acl.AddAccessRule($rule)
        }
        [IO.Directory]::SetAccessControl($SharePath, $acl)
        $result.ok = $true
        Finish
    }

    if ($Action -eq 'dns') {
        if (-not (Get-Module -ListAvailable -Name DnsServer)) { throw 'dns_module_missing' }
        Import-Module DnsServer
        $server = Get-Pdc
        $result.dc = $server
        $t = Find-DnsTarget $server
        $state = Get-DnsState $t
        $result.dns_existing = @($t.a)
        if ($state -eq 'conflict') { throw 'dns_conflict' }
        if ($state -eq 'new_zone') {
            Add-DnsServerPrimaryZone -ComputerName $server -Name $DnsName -ReplicationScope Domain | Out-Null
            Add-DnsServerResourceRecordA -ComputerName $server -ZoneName $DnsName -Name '@' -IPv4Address $DnsIp | Out-Null
            $result.dns_zone = $DnsName
        }
        elseif ($state -eq 'add') {
            Add-DnsServerResourceRecordA -ComputerName $server -ZoneName $t.zone -Name $t.rel -IPv4Address $DnsIp | Out-Null
            $result.dns_zone = $t.zone
        }
        else { $result.dns_zone = $t.zone }
        $result.dns_state = $state
        $result.ok = $true
        Finish
    }

    if (-not $result.gp_module) { throw 'gp_module_missing' }
    if (-not $GpoName) { throw 'gpo_name_missing' }
    Import-Module GroupPolicy

    # One domain controller for every read and write below. Mixing controllers means
    # reading a GPO that has not replicated yet, or writing SYSVOL on one DC and the
    # directory object on another.
    $dc = Get-Pdc
    $result.dc = $dc

    $domainDn = (($DomainFqdn.Split('.')) | ForEach-Object { 'DC=' + $_ }) -join ','
    if (-not $TargetDn) { $TargetDn = $domainDn }
    $gpo = Get-GPO -Name $GpoName -Domain $DomainFqdn -Server $dc -ErrorAction SilentlyContinue
    if ($gpo -and ($gpo.Description -ne $Marker)) { throw 'gpo_name_in_use' }

    if ($Action -eq 'remove') {
        if ($gpo) {
            try { Remove-GPLink -Guid $gpo.Id -Target $TargetDn -Domain $DomainFqdn -Server $dc | Out-Null } catch {}
            Remove-GPO -Guid $gpo.Id -Domain $DomainFqdn -Server $dc
            $result.gpo_removed = $true
        }
        else { $result.gpo_removed = $false }
        $result.ok = $true
        Finish
    }

    # ---- apply ----
    if (-not (Test-Path -LiteralPath $TaskXml)) { throw 'task_xml_missing' }

    $created = $false
    if (-not $gpo) {
        $gpo = New-GPO -Name $GpoName -Domain $DomainFqdn -Server $dc -Comment $Marker
        $created = $true
    }
    $id = '{' + $gpo.Id.ToString().ToUpper() + '}'
    $ad = [ADSI]("LDAP://$dc/CN=$id,CN=Policies,CN=System,$domainDn")

    # Registry settings first (Defender exclusion, panel certificate): the cmdlets
    # write Registry.pol and bump the version themselves, and the task step below
    # then settles the final extension list.
    if ($DefenderExclusion -eq 'on') {
        Set-GPRegistryValue -Guid $gpo.Id -Domain $DomainFqdn -Server $dc -Key $DefenderKey -ValueName 'Exclusions_Paths' -Type DWord -Value 1 | Out-Null
        foreach ($p in $DefenderPaths) {
            Set-GPRegistryValue -Guid $gpo.Id -Domain $DomainFqdn -Server $dc -Key ($DefenderKey + '\Paths') -ValueName $p -Type String -Value '0' | Out-Null
        }
    }
    else {
        foreach ($p in $DefenderPaths) {
            try { Remove-GPRegistryValue -Guid $gpo.Id -Domain $DomainFqdn -Server $dc -Key ($DefenderKey + '\Paths') -ValueName $p -ErrorAction Stop | Out-Null } catch {}
        }
        try { Remove-GPRegistryValue -Guid $gpo.Id -Domain $DomainFqdn -Server $dc -Key $DefenderKey -ValueName 'Exclusions_Paths' -ErrorAction Stop | Out-Null } catch {}
    }
    $result.defender_exclusion = ($DefenderExclusion -eq 'on')

    # Panel certificate. Thumbprints this GPO trusted before that are not the
    # current certificate are removed, so a replaced certificate stops being trusted.
    $blob = $null
    if ($TrustCert) {
        $c = New-Object Security.Cryptography.X509Certificates.X509Certificate2(, [IO.File]::ReadAllBytes($TrustCert))
        if ($c.Thumbprint -ne $TrustThumb) { throw 'cert_mismatch' }
        # The registry value holds the certificate as a serialized store element,
        # which is exactly what SerializedCert produces.
        $blob = $c.Export([Security.Cryptography.X509Certificates.X509ContentType]::SerializedCert)
    }
    $trusted = @()
    try { $trusted = @(Get-GPRegistryValue -Guid $gpo.Id -Domain $DomainFqdn -Server $dc -Key $RootKey -ErrorAction Stop) } catch {}
    foreach ($e in $trusted) {
        $thumb = ([string]$e.FullKeyPath).Split('\')[-1]
        if (($thumb -match '^[0-9A-Fa-f]{40}$') -and ($thumb -ne $TrustThumb)) {
            try { Remove-GPRegistryValue -Guid $gpo.Id -Domain $DomainFqdn -Server $dc -Key ($RootKey + '\' + $thumb) -ValueName 'Blob' -ErrorAction Stop | Out-Null } catch {}
        }
    }
    if ($blob) {
        $cur = $null
        try { $cur = (Get-GPRegistryValue -Guid $gpo.Id -Domain $DomainFqdn -Server $dc -Key ($RootKey + '\' + $TrustThumb) -ValueName 'Blob' -ErrorAction Stop).Value } catch {}
        if (-not ($cur -is [byte[]]) -or ([Convert]::ToBase64String($cur) -ne [Convert]::ToBase64String($blob))) {
            Set-GPRegistryValue -Guid $gpo.Id -Domain $DomainFqdn -Server $dc -Key ($RootKey + '\' + $TrustThumb) -ValueName 'Blob' -Type Binary -Value $blob | Out-Null
        }
        $result.trust_installed = $true
    }
    if (($DefenderExclusion -eq 'on') -or $blob) { $wantExt = $FullCse } else { $wantExt = $TaskCse }
    $ad.RefreshCache()
    $curExt = [string]$ad.Properties['gPCMachineExtensionNames'].Value

    $policyDir = "\\$dc\SYSVOL\$DomainFqdn\Policies\$id"
    $taskDir = Join-Path $policyDir 'Machine\Preferences\ScheduledTasks'
    New-Item -ItemType Directory -Force -Path $taskDir | Out-Null
    $dest = Join-Path $taskDir 'ScheduledTasks.xml'

    $changed = $true
    if (Test-Path -LiteralPath $dest) {
        $changed = ((Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $TaskXml -Algorithm SHA256).Hash)
    }
    if ($changed) { Copy-Item -LiteralPath $TaskXml -Destination $dest -Force }

    if ($changed -or ($curExt -ne $wantExt)) {
        # versionNumber: high word = user version, low word = computer version.
        # Clients re-read the policy only when the computer version moves, and the
        # directory value must match GPT.INI in SYSVOL.
        $ver = [int]$ad.Properties['versionNumber'].Value
        $ver = $ver + 1
        $ad.Properties['gPCMachineExtensionNames'].Value = $wantExt
        $ad.Properties['versionNumber'].Value = $ver
        $ad.CommitChanges()

        $ini = Join-Path $policyDir 'GPT.INI'
        $lines = @()
        if (Test-Path -LiteralPath $ini) { $lines = @(Get-Content -LiteralPath $ini) }
        if ($lines -match '^\s*Version\s*=') {
            $lines = $lines | ForEach-Object { if ($_ -match '^\s*Version\s*=') { "Version=$ver" } else { $_ } }
        }
        else {
            if (-not ($lines -match '^\[General\]')) { $lines = @('[General]') + $lines }
            $lines = $lines + "Version=$ver"
        }
        Set-Content -LiteralPath $ini -Value $lines -Encoding Ascii
        $result.version = $ver
    }

    # Computer-only policy: skip user-side processing on every logon.
    if ($gpo.GpoStatus -ne 'UserSettingsDisabled') { $gpo.GpoStatus = 'UserSettingsDisabled' }

    $linked = $false
    $inh = Get-GPInheritance -Target $TargetDn -Domain $DomainFqdn -Server $dc
    foreach ($l in $inh.GpoLinks) { if ($l.GpoId -eq $gpo.Id) { $linked = $true } }
    if (-not $linked) {
        New-GPLink -Guid $gpo.Id -Target $TargetDn -Domain $DomainFqdn -Server $dc -LinkEnabled Yes | Out-Null
    }

    $result.gpo_id = $gpo.Id.ToString()
    $result.gpo_created = $created
    $result.task_updated = $changed
    $result.link_created = (-not $linked)
    $result.target = $TargetDn
    $result.ok = $true
    Finish
}
catch {
    # Known conditions are thrown as bare codes; anything else keeps its text so the
    # admin can read what the domain controller said.
    $result.ok = $false
    $result.error = [string]$_.Exception.Message
    Finish
}
