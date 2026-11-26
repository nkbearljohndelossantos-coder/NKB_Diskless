# ==============================================================================
# NKB DISKLESS - TRANSACTIONAL NETWORK STAGING SCRIPT: NKB Zero-Purchase Isolated Staging
# Target Adapter: Ethernet (Realtek Gaming GbE Family Controller, MAC: B0-0C-D1-E6-53-31)
# Dedicated IP: 192.168.50.1/24 (Zero Default Gateway)
# Configuration-Driven, Snapshot-Driven, Parameterized
# Requires Elevated Administrator Privileges
# ==============================================================================

[CmdletBinding()]
param(
    [Parameter(Mandatory=$false)]
    [string]$ProfilePath = "config/network_profile.json",
    [Parameter(Mandatory=$false)]
    [string]$SnapshotDir = "config/snapshots"
)

$ErrorActionPreference = 'Stop'

$alias = "Ethernet"
$mac = "B0-0C-D1-E6-53-31"
$guid = "{620271F0-ABF2-492B-B226-1871CF04E0BB}"
$targetIP = "192.168.50.1"
$prefixLength = 24
$persistentKey = "nic_9ac91b948192645b"

Write-Host '======================================================================' -ForegroundColor Cyan
Write-Host ' NKB DISKLESS TRANSACTIONAL STAGING' -ForegroundColor Cyan
Write-Host " Target: $alias ($mac) -> $targetIP/$prefixLength" -ForegroundColor Cyan
Write-Host '======================================================================' -ForegroundColor Cyan

# ------------------------------------------------------------------------------
# PHASE 1: PREFLIGHT VALIDATION & MANAGEMENT ROUTE INDEPENDENCE CHECK
# ------------------------------------------------------------------------------
Write-Host "`n[PHASE 1/4] Preflight verification..." -ForegroundColor White
$adapters = Get-NetAdapter -ErrorAction SilentlyContinue
$cleanTargetMac = ($mac -replace '[-:]', '').ToUpper()
$matchedAdapters = @()
foreach ($a in $adapters) {
    $currMac = ($a.MacAddress -replace '[-:]', '').ToUpper()
    if ($currMac -eq $cleanTargetMac -or ($guid -and $a.InterfaceGuid -eq $guid)) {
        $matchedAdapters += $a
    }
}

if ($matchedAdapters.Count -eq 0) {
    Write-Host "[BLOCKED] Target adapter not found (MAC: $mac, GUID: $guid). Aborting." -ForegroundColor Red
    exit 1
}
if ($matchedAdapters.Count -gt 1) {
    Write-Host "[BLOCKED] Ambiguous target adapter match! $($matchedAdapters.Count) adapters matched MAC $mac or GUID $guid. Fail-closed." -ForegroundColor Red
    exit 1
}
$adapter = $matchedAdapters[0]

# Reject wireless interfaces
if ($adapter.PhysicalMediaType -eq 'Native 802.11' -or $adapter.Name -like '*Wi-Fi*' -or $adapter.InterfaceDescription -like '*Wireless*') {
    Write-Host "[BLOCKED] Target adapter '$($adapter.Name)' is a wireless interface. Staging prohibited." -ForegroundColor Red
    exit 1
}

# Verify management route independence (Task 2 multi-point verification)
$targetDefaultRoutes = Get-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue
$hasTargetGW = ($null -ne $targetDefaultRoutes -and $targetDefaultRoutes.Count -gt 0)
if ($hasTargetGW) {
    Write-Host "Target adapter holds default gateway. Verifying alternate management route independence..." -ForegroundColor Yellow
    $altCandidates = Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object {
        $_.InterfaceIndex -ne $adapter.InterfaceIndex -and $_.Status -eq 'Up'
    }
    $validAlt = $null
    foreach ($cand in $altCandidates) {
        $candIPs = Get-NetIPAddress -InterfaceIndex $cand.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {
            $_.IPAddress -notlike '169.254.*' -and $_.IPAddress -ne '0.0.0.0' -and ($_.AddressState -eq 'Preferred' -or -not $_.AddressState)
        }
        if (-not $candIPs) { continue }

        $candRoutes = Get-NetRoute -InterfaceIndex $cand.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object {
            $_.NextHop -and $_.NextHop -ne '0.0.0.0'
        }
        if (-not $candRoutes) { continue }

        $probeTarget = $candRoutes[0].NextHop
        $reachable = $false
        try {
            $pingOk = Test-Connection -ComputerName $probeTarget -Count 2 -Quiet -ErrorAction SilentlyContinue
            if ($pingOk) {
                $reachable = $true
            } else {
                $neighbor = Get-NetNeighbor -InterfaceIndex $cand.InterfaceIndex -IPAddress $probeTarget -ErrorAction SilentlyContinue | Where-Object {
                    $_.State -ne 'Unreachable' -and $_.State -ne 'None'
                }
                if ($neighbor) { $reachable = $true }
            }
        } catch {
            $reachable = $false
        }

        if ($reachable) {
            $validAlt = $cand
            break
        }
    }

    if (-not $validAlt) {
        Write-Host "[BLOCKED] Target adapter holds the host default gateway and no verified, reachable alternate management interface was found." -ForegroundColor Red
        Write-Host "Staging is BLOCKED to prevent severing host internet connectivity." -ForegroundColor Red
        exit 2
    }
    Write-Host "Alternate management interface '$($validAlt.Name)' (Index: $($validAlt.InterfaceIndex)) verified Up with reachable gateway. Host connectivity secured." -ForegroundColor Green
}

# ------------------------------------------------------------------------------
# PHASE 2: CAPTURE PERSISTENT WINDOWS CONFIGURATION SNAPSHOT (LIVE CIM)
# ------------------------------------------------------------------------------
Write-Host "`n[PHASE 2/4] Capturing persistent Windows adapter configuration snapshot..." -ForegroundColor White
if (-not (Test-Path $SnapshotDir)) { New-Item -ItemType Directory -Path $SnapshotDir -Force | Out-Null }

$idx = $adapter.InterfaceIndex
$ipConf = Get-NetIPConfiguration -InterfaceIndex $idx -ErrorAction SilentlyContinue
$v4Int = Get-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv4 -ErrorAction SilentlyContinue
$v6Int = Get-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv6 -ErrorAction SilentlyContinue
$connProfile = Get-NetConnectionProfile -InterfaceIndex $idx -ErrorAction SilentlyContinue
$dnsClient = Get-DnsClient -InterfaceIndex $idx -ErrorAction SilentlyContinue

$dhcpV4 = ($v4Int -and $v4Int.Dhcp -eq 'Enabled')
$dhcpV6 = ($v6Int -and $v6Int.Dhcp -eq 'Enabled')
$metric4 = if ($v4Int) { [int]$v4Int.InterfaceMetric } else { 0 }
$metric6 = if ($v6Int) { [int]$v6Int.InterfaceMetric } else { 0 }
$autoMetric4 = if ($v4Int) { [bool]($v4Int.AutomaticMetric -eq 'Enabled') } else { $false }
$autoMetric6 = if ($v6Int) { [bool]($v6Int.AutomaticMetric -eq 'Enabled') } else { $false }
$regDns = if ($dnsClient) { [bool]$dnsClient.RegisterThisConnectionsAddress } else { $true }
$netCat = if ($connProfile -and $connProfile.NetworkCategory) { [string]$connProfile.NetworkCategory } else { 'Private' }

$snapIPs = @()
if ($ipConf -and $ipConf.IPv4Address) {
    $snapIPs = @($ipConf.IPv4Address | ForEach-Object { @{ ip_address = $_.IPAddress; prefix_length = [int]$_.PrefixLength } })
}
$snapV6 = @()
if ($ipConf -and $ipConf.IPv6Address) {
    $snapV6 = @($ipConf.IPv6Address.IPAddress)
}
$snapGWs = @()
if ($ipConf -and $ipConf.IPv4DefaultGateway) {
    $snapGWs = @($ipConf.IPv4DefaultGateway.NextHop)
}
$snapDNS = @()
if ($ipConf -and $ipConf.DNSServer) {
    $snapDNS = @($ipConf.DNSServer.ServerAddresses)
}

$nowTimestamp = Get-Date -Format 'yyyyMMdd_HHmmss'
$snapId = "snap_${persistentKey}_$nowTimestamp"
$snapshotPath = Join-Path $SnapshotDir "$snapId.json"

$machineGuid = (Get-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Cryptography' -ErrorAction SilentlyContinue).MachineGuid
$hostIdentity = if ($machineGuid) { "$($env:COMPUTERNAME)|$machineGuid" } else { $env:COMPUTERNAME }

$ipStrs = @()
if ($snapIPs) { $ipStrs = @($snapIPs | ForEach-Object { "$($_.ip_address)/$($_.prefix_length)" } | Sort-Object) }
$v6Sorted = @($snapV6 | Sort-Object)
$gwSorted = @($snapGWs | Sort-Object)
$dnsSorted = @($snapDNS | Sort-Object)
$cleanMac = ($adapter.MacAddress -replace '[-:]', '').ToUpper()
$guidUpper = if ($adapter.InterfaceGuid) { $adapter.InterfaceGuid.ToUpper() } else { '' }

$canonStr = "ID:$snapId|KEY:$persistentKey|GUID:$guidUpper|MAC:$cleanMac|INDEX:$idx|DHCP4:$dhcpV4|DHCP6:$dhcpV6|IPS:$(-join ($ipStrs, ','))|IPV6:$(-join ($v6Sorted, ','))|GWS:$(-join ($gwSorted, ','))|DNS:$(-join ($dnsSorted, ','))|METRIC4:$metric4|METRIC6:$metric6|REGDNS:$regDns|CAT:$netCat|SYNTH:False|PROV:LIVE_WINDOWS_CIM|HOST:$hostIdentity"
$hasher = [System.Security.Cryptography.SHA256]::Create()
$hashBytes = $hasher.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($canonStr))
$calcIntegrityHash = "sha256:" + (-join ($hashBytes | ForEach-Object { "{0:x2}" -f $_ }))

$snapshotObj = [PSCustomObject]@{
    snapshot_id = $snapId
    timestamp = (Get-Date).ToUniversalTime().ToString('o')
    persistent_key = $persistentKey
    interface_guid = $adapter.InterfaceGuid
    interface_index = $idx
    interface_alias = $adapter.Name
    mac_address = $adapter.MacAddress
    hardware_description = $adapter.InterfaceDescription
    description = $adapter.InterfaceDescription
    status = $adapter.Status
    dhcp_enabled = $dhcpV4
    dhcpv4_enabled = $dhcpV4
    dhcpv6_enabled = $dhcpV6
    ipv4_addresses = $snapIPs
    ipv6_addresses = $snapV6
    default_gateways = $snapGWs
    ipv4_default_gateways = $snapGWs
    ipv4_dns_servers = $snapDNS
    dns_servers = $snapDNS
    interface_metric_ipv4 = $metric4
    interface_metric_ipv6 = $metric6
    automatic_metric_ipv4 = $autoMetric4
    automatic_metric_ipv6 = $autoMetric6
    register_this_connections_address = $regDns
    network_category = $netCat
    is_synthetic = $false
    provenance = "LIVE_WINDOWS_CIM"
    host_identity = $hostIdentity
    integrity_hash = $calcIntegrityHash
}

$snapshotJson = $snapshotObj | ConvertTo-Json -Depth 5
$tmpFile = "$snapshotPath.tmp"
$snapshotJson | Set-Content -Path $tmpFile -Encoding UTF8 -Force
Move-Item -Path $tmpFile -Destination $snapshotPath -Force

# Verify snapshot integrity on disk
if (-not (Test-Path $snapshotPath) -or (Get-Item $snapshotPath).Length -eq 0) {
    Write-Host "[BLOCKED] Snapshot file could not be written to disk! Aborting before changes." -ForegroundColor Red
    exit 2
}
Write-Host "Persistent live CIM snapshot saved and verified: $snapshotPath" -ForegroundColor Green

# ------------------------------------------------------------------------------
# PHASE 3: APPLY STAGING CONFIGURATION WITH TRANSACTIONAL ROLLBACK
# ------------------------------------------------------------------------------
Write-Host "`n[PHASE 3/4] Applying static staging configuration..." -ForegroundColor White
try {
    Write-Host " - Clearing existing default routes on '$($adapter.Name)'..." -ForegroundColor Gray
    Get-NetRoute -InterfaceIndex $idx -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue

    Write-Host " - Clearing existing IPv4 addresses on '$($adapter.Name)'..." -ForegroundColor Gray
    Get-NetIPAddress -InterfaceIndex $idx -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false

    Write-Host " - Assigning dedicated static IP $targetIP/$prefixLength (Zero Gateway)..." -ForegroundColor Gray
    # Enforce zero default gateway: -DefaultGateway $null (air-gap isolation requires zero default gateway)
    New-NetIPAddress -InterfaceIndex $idx -IPAddress $targetIP -PrefixLength $prefixLength -ErrorAction Stop | Out-Null

    Write-Host " - Enforcing empty DNS and Private network profile..." -ForegroundColor Gray
    Set-DnsClientServerAddress -InterfaceIndex $idx -ServerAddresses @() -ErrorAction SilentlyContinue
    Set-NetConnectionProfile -InterfaceIndex $idx -NetworkCategory Private -ErrorAction SilentlyContinue
    Clear-DnsClientCache -ErrorAction SilentlyContinue

    # ------------------------------------------------------------------------------
    # PHASE 4: VERIFICATION GATE
    # ------------------------------------------------------------------------------
    Write-Host "`n[PHASE 4/4] Verifying applied configuration..." -ForegroundColor White
    $verifiedIP = Get-NetIPAddress -InterfaceIndex $idx -IPAddress $targetIP -ErrorAction SilentlyContinue
    $verifiedGW = Get-NetRoute -InterfaceIndex $idx -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue

    if (-not $verifiedIP) {
        throw "Target static IP $targetIP/$prefixLength could not be verified on '$($adapter.Name)'."
    }
    if ($verifiedGW) {
        throw "Default gateway violation: isolated network mode requires zero default gateway."
    }

    Write-Host '`n======================================================================' -ForegroundColor Green
    Write-Host " [PASS] Staging transaction succeeded for '$($adapter.Name)' ($targetIP/$prefixLength)." -ForegroundColor Green
    Write-Host '======================================================================' -ForegroundColor Green
    exit 0
}
catch {
    Write-Host "`n[ERROR] Staging failed: $_" -ForegroundColor Red
    Write-Host "[ROLLBACK] Invoking rollback script with persistent snapshot..." -ForegroundColor Yellow

    $rollbackScript = Join-Path $PSScriptRoot 'rollback_adapter.ps1'
    if (-not (Test-Path $rollbackScript)) {
        $rollbackScript = 'config\scripts\rollback_adapter.ps1'
    }
    if (Test-Path $rollbackScript) {
        & $rollbackScript -SnapshotPath $snapshotPath
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[ROLLBACK_SUCCESS] Automated rollback successfully restored baseline." -ForegroundColor Green
            exit 3
        } else {
            Write-Host "[ROLLBACK_FAILED] Automated rollback reported failure code $LASTEXITCODE." -ForegroundColor Red
            exit 4
        }
    } else {
        Write-Host "[ROLLBACK_FAILED] Rollback script not found at $rollbackScript!" -ForegroundColor Red
        exit 4
    }
}
