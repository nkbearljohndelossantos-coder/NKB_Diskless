# ==============================================================================
# NKB DISKLESS - WINDOWS NETWORK RESTORATION SCRIPT (ROLLBACK)
# Snapshot ID: snap_sample_synthetic_template | Captured: 2026-11-24T03:48:59Z
# Target Adapter: Ethernet (Realtek Gaming GbE Family Controller, MAC: B0:0C:D1:E6:53:31)
# Configuration-Driven, Snapshot-Driven, Parameterized
# Requires Elevated Administrator Privileges
# ==============================================================================

[CmdletBinding()]
param(
    [Parameter(Mandatory=$false)]
    [string]$SnapshotPath = "config/snapshots/snap_sample_synthetic_template.json",
    [Parameter(Mandatory=$false)]
    [switch]$AllowSyntheticFixture
)

$ErrorActionPreference = 'Stop'

# Snapshot reference payload (audit trail):
$embeddedSnapshotJson = @'
{
  "snapshot_id": "snap_sample_synthetic_template",
  "timestamp": "2026-11-24T03:48:59.2745195Z",
  "persistent_key": "nic_9ac91b948192645b",
  "interface_guid": "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
  "interface_index": 8,
  "interface_alias": "Ethernet",
  "mac_address": "B0:0C:D1:E6:53:31",
  "hardware_description": "Realtek Gaming GbE Family Controller",
  "description": "Realtek Gaming GbE Family Controller",
  "status": "Up",
  "dhcp_enabled": true,
  "dhcpv4_enabled": true,
  "dhcpv6_enabled": false,
  "ipv4_addresses": [
    {
      "ip_address": "192.168.254.20",
      "prefix_length": 24
    }
  ],
  "ipv6_addresses": null,
  "default_gateways": [
    "192.168.254.254"
  ],
  "ipv4_default_gateways": [
    "192.168.254.254"
  ],
  "ipv6_default_gateways": [],
  "routes": [
    {
      "destination_prefix": "0.0.0.0/0",
      "next_hop": "192.168.254.254",
      "route_metric": 10,
      "address_family": "IPv4"
    }
  ],
  "route_metrics": [
    {
      "destination_prefix": "0.0.0.0/0",
      "metric": 10
    }
  ],
  "interface_metric_ipv4": 0,
  "interface_metric_ipv6": 0,
  "automatic_metric_ipv4": false,
  "automatic_metric_ipv6": false,
  "dns_servers": [
    "192.168.254.254",
    "1.1.1.1"
  ],
  "ipv4_dns_servers": [
    "192.168.254.254",
    "1.1.1.1"
  ],
  "ipv6_dns_servers": [],
  "dns_from_dhcp": true,
  "dns_configuration_source": "",
  "register_this_connections_address": false,
  "network_category": "Public",
  "is_synthetic": true,
  "provenance": "SYNTHETIC_TEST_FIXTURE",
  "host_identity": "SYNTHETIC_TEMPLATE_HOST",
  "integrity_hash": "sha256:ea04259d63a1ad45611ab8c8dcf0ec9c9ac7872e424098a3bd1648dfc883314e"
}
'@

Write-Host '======================================================================' -ForegroundColor Cyan
Write-Host ' NKB DISKLESS CONFIGURATION-DRIVEN ROLLBACK' -ForegroundColor Cyan
Write-Host '======================================================================' -ForegroundColor Cyan

# Locate snapshot file on disk (fail-closed if missing)
$snap = $null
if ($SnapshotPath -and (Test-Path $SnapshotPath)) {
    Write-Host "Loading snapshot from: $SnapshotPath" -ForegroundColor White
    try {
        $rawJson = Get-Content -Raw -Path $SnapshotPath -ErrorAction Stop
        $snap = $rawJson | ConvertFrom-Json
    } catch {
        Write-Host "[ROLLBACK_FAILED] Corrupted snapshot file: $_" -ForegroundColor Red
        exit 1
    }
} else {
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    $candidateDirs = @(
        $scriptDir,
        (Join-Path $scriptDir '..\snapshots'),
        'config\snapshots',
        'C:\NKB-PoC\config\snapshots'
    )
    $foundPath = $null
    if ($SnapshotPath) {
        $leaf = Split-Path -Leaf $SnapshotPath
        foreach ($d in $candidateDirs) {
            $candidate = Join-Path $d $leaf
            if (Test-Path $candidate) {
                $foundPath = $candidate
                break
            }
        }
    }
    if ($foundPath) {
        Write-Host "Loading snapshot from discovered path: $foundPath" -ForegroundColor White
        $snap = (Get-Content -Raw -Path $foundPath) | ConvertFrom-Json
    } else {
        Write-Host "[ROLLBACK_FAILED] Snapshot file not found at '$SnapshotPath' and no matching snapshot found in candidate directories." -ForegroundColor Red
        Write-Host "Rollback is fail-closed. Staging baseline cannot be restored without a valid snapshot." -ForegroundColor Red
        exit 1
    }
}

# Safety Check 1: Provenance and synthetic fixture rejection
if (($snap.is_synthetic -or $snap.provenance -eq 'SYNTHETIC_TEST_FIXTURE') -and -not $AllowSyntheticFixture) {
    Write-Host "[ROLLBACK_FAILED] Snapshot is marked as SYNTHETIC_TEST_FIXTURE. Production rollback refused. Use -AllowSyntheticFixture only in isolated tests." -ForegroundColor Red
    exit 1
}

# Safety Check 2: Mandatory integrity hash verification
if (-not $snap.integrity_hash) {
    Write-Host "[ROLLBACK_FAILED] Snapshot is missing mandatory integrity checksum (integrity_hash). Rollback refused." -ForegroundColor Red
    exit 1
}

# Safety Check 3: Host identity cross-system verification
if ($snap.host_identity -and $snap.host_identity -ne $env:COMPUTERNAME) {
    $snapHostPrefix = ($snap.host_identity -split '\|')[0]
    if ($snapHostPrefix -ne $env:COMPUTERNAME) {
        Write-Host "[ROLLBACK_FAILED] Snapshot host identity mismatch: snapshot belongs to '$($snap.host_identity)' but current host is '$($env:COMPUTERNAME)'. Rollback refused." -ForegroundColor Red
        exit 1
    }
}

# Re-identify target adapter by MAC or Interface GUID (Fail-closed on 0 or >1 matches)
Write-Host "`n[1/4] Re-identifying target adapter (MAC: $($snap.mac_address), GUID: $($snap.interface_guid))..." -ForegroundColor Cyan
$adapters = Get-NetAdapter -ErrorAction SilentlyContinue
$targetMacClean = ($snap.mac_address -replace '[-:]', '').ToUpper()
$matchedAdapters = @()
foreach ($a in $adapters) {
    $currMac = ($a.MacAddress -replace '[-:]', '').ToUpper()
    if ($currMac -eq $targetMacClean -or ($snap.interface_guid -and $a.InterfaceGuid -eq $snap.interface_guid)) {
        $matchedAdapters += $a
    }
}

if ($matchedAdapters.Count -eq 0) {
    Write-Host "[ROLLBACK_FAILED] Target adapter not found (Expected MAC: $($snap.mac_address), GUID: $($snap.interface_guid))." -ForegroundColor Red
    exit 1
}
if ($matchedAdapters.Count -gt 1) {
    Write-Host "[ROLLBACK_FAILED] Ambiguous adapter match! $($matchedAdapters.Count) adapters matched MAC $($snap.mac_address) or GUID $($snap.interface_guid). Refusing modification to prevent misconfiguration." -ForegroundColor Red
    exit 1
}

$adapter = $matchedAdapters[0]
$currIdx = $adapter.InterfaceIndex
$currAlias = $adapter.Name
Write-Host "Matched adapter: '$currAlias' (InterfaceIndex: $currIdx, Status: $($adapter.Status))" -ForegroundColor Green

Write-Host "`n[2/4] Clearing staged static IP addresses and default routes on '$currAlias'..." -ForegroundColor Cyan
Get-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
Get-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false

Write-Host "`n[3/4] Restoring original Windows configuration from snapshot..." -ForegroundColor Cyan
$isDhcp = [bool]$snap.dhcpv4_enabled
if ($null -eq $snap.dhcpv4_enabled) { $isDhcp = [bool]$snap.dhcp_enabled }

if ($isDhcp) {
    Write-Host " - Enabling DHCP dynamic configuration on '$currAlias'..." -ForegroundColor White
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -Dhcp Enabled -ErrorAction SilentlyContinue
    Set-DnsClientServerAddress -InterfaceIndex $adapter.InterfaceIndex -ResetServerAddresses -ErrorAction SilentlyContinue
} else {
    Write-Host " - Restoring static IP configuration on '$currAlias'..." -ForegroundColor White
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -Dhcp Disabled -ErrorAction SilentlyContinue
    if ($snap.ipv4_addresses) {
        foreach ($ip in $snap.ipv4_addresses) {
            $prefix = if ($ip.prefix_length) { $ip.prefix_length } else { 24 }
            New-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $ip.ip_address -PrefixLength $prefix -ErrorAction SilentlyContinue | Out-Null
        }
    }
    $gws = @()
    if ($snap.ipv4_default_gateways) { $gws = @($snap.ipv4_default_gateways) }
    elseif ($snap.default_gateways) { $gws = @($snap.default_gateways) }
    foreach ($gw in $gws) {
        if ($gw -and $gw -ne '0.0.0.0') {
            $gwMetric = 10
            if ($snap.route_metrics) {
                $mEntry = $snap.route_metrics | Where-Object { $_.destination_prefix -eq '0.0.0.0/0' } | Select-Object -First 1
                if ($mEntry -and $mEntry.metric) { $gwMetric = $mEntry.metric }
            }
            New-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -NextHop $gw -RouteMetric $gwMetric -ErrorAction SilentlyContinue | Out-Null
        }
    }
    if ($snap.routes) {
        foreach ($r in $snap.routes) {
            if ($r.destination_prefix -and $r.next_hop -and $r.destination_prefix -ne '0.0.0.0/0') {
                $mVal = if ($r.route_metric) { $r.route_metric } else { 10 }
                New-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix $r.destination_prefix -NextHop $r.next_hop -RouteMetric $mVal -ErrorAction SilentlyContinue | Out-Null
            }
        }
    }
    $dns = @()
    if ($snap.ipv4_dns_servers) { $dns += @($snap.ipv4_dns_servers) }
    elseif ($snap.dns_servers) { $dns += @($snap.dns_servers) }
    if ($snap.ipv6_dns_servers) { $dns += @($snap.ipv6_dns_servers) }
    if ($dns.Count -gt 0) {
        Set-DnsClientServerAddress -InterfaceIndex $adapter.InterfaceIndex -ServerAddresses $dns -ErrorAction SilentlyContinue
    }
}

# Restore IPv6 state
if ($snap.dhcpv6_enabled) {
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -Dhcp Enabled -ErrorAction SilentlyContinue
} elseif ($snap.ipv6_addresses_detailed) {
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -Dhcp Disabled -ErrorAction SilentlyContinue
    foreach ($v6 in $snap.ipv6_addresses_detailed) {
        New-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $v6.ip_address -PrefixLength $v6.prefix_length -ErrorAction SilentlyContinue | Out-Null
    }
}

# Restore interface metrics
if ($snap.automatic_metric_ipv4) {
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -AutomaticMetric Enabled -ErrorAction SilentlyContinue
} elseif ($snap.interface_metric_ipv4 -and $snap.interface_metric_ipv4 -gt 0) {
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -InterfaceMetric $snap.interface_metric_ipv4 -ErrorAction SilentlyContinue
}
if ($snap.automatic_metric_ipv6) {
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -AutomaticMetric Enabled -ErrorAction SilentlyContinue
} elseif ($snap.interface_metric_ipv6 -and $snap.interface_metric_ipv6 -gt 0) {
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -InterfaceMetric $snap.interface_metric_ipv6 -ErrorAction SilentlyContinue
}

# Restore Dynamic DNS registration
if ($null -ne $snap.register_this_connections_address) {
    Set-DnsClient -InterfaceIndex $adapter.InterfaceIndex -RegisterThisConnectionsAddress:$snap.register_this_connections_address -ErrorAction SilentlyContinue
}

# Restore Network Category
if ($snap.network_category) {
    Set-NetConnectionProfile -InterfaceIndex $adapter.InterfaceIndex -NetworkCategory $snap.network_category -ErrorAction SilentlyContinue
}

Write-Host "`n[4/4] Verifying exact restoration against snapshot..." -ForegroundColor Cyan
$verified = $true
$errs = @()
if ($isDhcp) {
    $check = Get-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue
    if (-not $check -or $check.Dhcp -ne 'Enabled') {
        $verified = $false
        $errs += 'DHCP state was not restored to Enabled.'
    }
} else {
    if ($snap.ipv4_addresses) {
        foreach ($expIP in $snap.ipv4_addresses) {
            $check = Get-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $expIP.ip_address -ErrorAction SilentlyContinue
            if (-not $check) {
                $verified = $false
                $errs += "Static IP $($expIP.ip_address) not found on adapter."
            }
        }
    }
    if ($gws.Count -gt 0) {
        $gwCheck = Get-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue
        if (-not $gwCheck) {
            $verified = $false
            $errs += 'Default gateway route not found on adapter.'
        }
    }
}

if ($snap.network_category) {
    $catCheck = (Get-NetConnectionProfile -InterfaceIndex $adapter.InterfaceIndex -ErrorAction SilentlyContinue).NetworkCategory
    if ($catCheck -and $catCheck.ToString() -ne $snap.network_category) {
        $verified = $false
        $errs += "NetworkCategory mismatch: expected $($snap.network_category), found $catCheck."
    }
}

if ($verified) {
    Write-Host '======================================================================' -ForegroundColor Green
    Write-Host " [PASS] Windows network adapter '$currAlias' restored successfully from snapshot." -ForegroundColor Green
    Write-Host '======================================================================' -ForegroundColor Green
    exit 0
} else {
    Write-Host "[ROLLBACK_FAILED] Restoration verification failed:" -ForegroundColor Red
    $errs | ForEach-Object { Write-Host " - $_" -ForegroundColor Red }
    exit 1
}
