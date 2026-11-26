<#
.SYNOPSIS
    NKB Diskless System — First-Run Hardware Onboarding & Adapter Selection Wizard
.DESCRIPTION
    Discovers physical network adapters, verifies management route independence,
    and configures the diskless network profile without hardcoded hardware dependencies.
    
    SAFETY RULE: Does NOT modify live network settings or start PXE listeners.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$InstallDir = $PSScriptRoot,

    [Parameter(Mandatory = $false)]
    [string]$TargetMAC,

    [Parameter(Mandatory = $false)]
    [ValidateSet("Full", "StorageNode", "ManagementNode")]
    [string]$ServerRole = "Full",

    [Parameter(Mandatory = $false)]
    [string]$StorageRoot = "C:\NKB-PoC",

    [Parameter(Mandatory = $false)]
    [string]$DisklessIP = "192.168.50.1",

    [Parameter(Mandatory = $false)]
    [string]$SubnetMask = "255.255.255.0",

    [Parameter(Mandatory = $false)]
    [string]$DHCPStart = "192.168.50.100",

    [Parameter(Mandatory = $false)]
    [string]$DHCPEnd = "192.168.50.200",

    [Parameter(Mandatory = $false)]
    [switch]$NonInteractive,

    [Parameter(Mandatory = $false)]
    [switch]$SimulatedNIC,

    [Parameter(Mandatory = $false)]
    [string]$SimulatedMAC = "00-1B-21-34-56-78",

    [Parameter(Mandatory = $false)]
    [string]$SimulatedAlias = "Intel-NIC-01",

    [Parameter(Mandatory = $false)]
    [int]$SimulatedIndex = 42,

    [Parameter(Mandatory = $false)]
    [string]$SimulatedDescription = "Intel(R) Ethernet Server Adapter I210-T1",

    [Parameter(Mandatory = $false)]
    [string]$SimulatedGUID = "{A1B2C3D4-E5F6-4A5B-8C9D-0E1F2A3B4C5D}"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "   NKB DISKLESS SYSTEM — FIRST-RUN HARDWARE ONBOARDING WIZARD   " -ForegroundColor Cyan
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "Installed Path: $InstallDir"
Write-Host "Storage Root  : $StorageRoot"
Write-Host ""

# Ensure config directories exist
$ConfigDir = Join-Path $InstallDir "config"
$SnapshotsDir = Join-Path $ConfigDir "snapshots"
$ScriptsDir = Join-Path $ConfigDir "scripts"
foreach ($dir in @($ConfigDir, $SnapshotsDir, $ScriptsDir)) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# 1. Discover Network Adapters
Write-Host "[1/5] Discovering host network adapters..." -ForegroundColor Yellow
$Adapters = Get-NetAdapter -Physical -ErrorAction SilentlyContinue | Where-Object { $_.HardwareInterface -eq $true }
if (-not $Adapters -or $Adapters.Count -eq 0) {
    # Fallback to all adapters if no physical flag
    $Adapters = Get-NetAdapter -ErrorAction SilentlyContinue
}

$DefaultRoutes = Get-NetRoute -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue

$AdapterChoices = @()
$Index = 0

Write-Host ""
Write-Host "Available Network Adapters:" -ForegroundColor Cyan
Write-Host "--------------------------------------------------------------------------------"
foreach ($nic in $Adapters) {
    $Index++
    $hasGateway = $false
    $matchingRoute = $DefaultRoutes | Where-Object { $_.InterfaceIndex -eq $nic.InterfaceIndex }
    if ($matchingRoute) {
        $hasGateway = $true
    }

    $ipAddrs = (Get-NetIPAddress -InterfaceIndex $nic.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue).IPAddress -join ", "
    if (-not $ipAddrs) { $ipAddrs = "<None>" }

    $isProtected = $false
    $roleTag = "Candidate"
    if ($hasGateway -or $nic.InterfaceAlias -match "Wi-Fi|Wireless|WLAN") {
        $isProtected = $true
        $roleTag = "PROTECTED (Active Management / Internet Gateway)"
    }

    $guid = if ($nic.InterfaceGuid) { $nic.InterfaceGuid } else { [Guid]::Empty.ToString("B") }

    $item = [PSCustomObject]@{
        Number         = $Index
        Alias          = $nic.InterfaceAlias
        Description    = $nic.InterfaceDescription
        MACAddress     = $nic.MacAddress
        InterfaceIndex = $nic.InterfaceIndex
        AdapterGUID    = $guid
        Status         = $nic.Status
        LinkSpeed      = $nic.LinkSpeed
        IPAddresses    = $ipAddrs
        HasGateway     = $hasGateway
        IsProtected    = $isProtected
        RoleTag        = $roleTag
    }
    $AdapterChoices += $item

    $color = if ($isProtected) { "Red" } else { "Green" }
    Write-Host (" [{0}] {1} ({2})" -f $Index, $nic.InterfaceAlias, $nic.InterfaceDescription) -ForegroundColor $color
    Write-Host ("     MAC: {0} | Index: {1} | Status: {2} | Speed: {3}" -f $nic.MacAddress, $nic.InterfaceIndex, $nic.Status, $nic.LinkSpeed)
    Write-Host ("     IPv4: {0} | Classification: {1}" -f $ipAddrs, $roleTag)
    Write-Host "--------------------------------------------------------------------------------"
}

if ($SimulatedNIC) {
    $Index++
    $simItem = [PSCustomObject]@{
        Number         = $Index
        Alias          = $SimulatedAlias
        Description    = $SimulatedDescription
        MACAddress     = $SimulatedMAC
        InterfaceIndex = $SimulatedIndex
        AdapterGUID    = $SimulatedGUID
        Status         = "Up"
        LinkSpeed      = "1 Gbps"
        IPAddresses    = "<Unconfigured>"
        HasGateway     = $false
        IsProtected    = $false
        RoleTag        = "Candidate (Simulated Replacement Hardware)"
    }
    $AdapterChoices += $simItem
    Write-Host (" [{0}] {1} ({2})" -f $Index, $SimulatedAlias, $SimulatedDescription) -ForegroundColor Green
    Write-Host ("     MAC: {0} | Index: {1} | GUID: {2} | Status: Up | Speed: 1 Gbps" -f $SimulatedMAC, $SimulatedIndex, $SimulatedGUID)
    Write-Host ("     IPv4: <Unconfigured> | Classification: Candidate (Simulated Replacement Hardware)")
    Write-Host "--------------------------------------------------------------------------------"
}

# 2. Select Adapter
$SelectedAdapter = $null
if ($TargetMAC) {
    $normTarget = $TargetMAC.Replace("-", ":").ToUpper().Trim()
    $SelectedAdapter = $AdapterChoices | Where-Object { $_.MACAddress.Replace("-", ":").ToUpper().Trim() -eq $normTarget } | Select-Object -First 1
    if (-not $SelectedAdapter) {
        throw "Target MAC $TargetMAC not found among discovered adapters."
    }
} elseif ($NonInteractive) {
    # Pick first non-protected candidate, or first adapter if none
    $SelectedAdapter = $AdapterChoices | Where-Object { -not $_.IsProtected } | Select-Object -First 1
    if (-not $SelectedAdapter) {
        $SelectedAdapter = $AdapterChoices | Select-Object -First 1
    }
} else {
    # Interactive selection
    while ($null -eq $SelectedAdapter) {
        $choice = Read-Host "Select adapter for Dedicated Diskless Network (1-$($AdapterChoices.Count))"
        if ($choice -match '^\d+$' -and [int]$choice -ge 1 -and [int]$choice -le $AdapterChoices.Count) {
            $cand = $AdapterChoices[[int]$choice - 1]
            if ($cand.IsProtected) {
                Write-Host ""
                Write-Host "WARNING: Adapter '$($cand.Alias)' is classified as PROTECTED MANAGEMENT." -ForegroundColor Red
                Write-Host "Modifying this adapter will disconnect your remote management or internet connectivity!" -ForegroundColor Red
                $confirm = Read-Host "Are you absolutely sure you want to select this adapter? (yes/NO)"
                if ($confirm -eq "yes") {
                    $SelectedAdapter = $cand
                }
            } else {
                $SelectedAdapter = $cand
            }
        } else {
            Write-Host "Invalid selection. Please enter a number between 1 and $($AdapterChoices.Count)." -ForegroundColor Yellow
        }
    }
}

Write-Host ""
Write-Host ("Selected Dedicated NIC: {0} ({1}) [MAC: {2}]" -f $SelectedAdapter.Alias, $SelectedAdapter.Description, $SelectedAdapter.MACAddress) -ForegroundColor Green

# 3. Verify Alternate Management Path Exists
$AltMgmt = $AdapterChoices | Where-Object { $_.MACAddress -ne $SelectedAdapter.MACAddress -and $_.Status -eq "Up" -and $_.HasGateway } | Select-Object -First 1
if (-not $AltMgmt) {
    Write-Host "CAUTION: No alternate active management adapter with default gateway was detected." -ForegroundColor Yellow
    Write-Host "Ensure an out-of-band management connection exists before staging." -ForegroundColor Yellow
} else {
    Write-Host ("Verified Alternate Management Path: {0} ({1})" -f $AltMgmt.Alias, $AltMgmt.IPAddresses) -ForegroundColor Cyan
}

# 4. Generate Hardware-Independent Network Profile
Write-Host ""
Write-Host "[3/5] Generating hardware-specific profile (config\network_profile.json)..." -ForegroundColor Yellow

# Parse subnet prefix length
$prefixLen = 24
if ($SubnetMask -eq "255.255.255.0") { $prefixLen = 24 }
elseif ($SubnetMask -eq "255.255.0.0") { $prefixLen = 16 }
elseif ($SubnetMask -eq "255.0.0.0") { $prefixLen = 8 }

$targetGuid = if ($SelectedAdapter.AdapterGUID) { $SelectedAdapter.AdapterGUID } else { [Guid]::Empty.ToString("B") }

$Profile = [PSCustomObject]@{
    version                = "1.0.0"
    schema_version         = "1.0.0"
    last_updated           = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")
    server_role            = $ServerRole
    storage_root           = $StorageRoot
    target_adapter = [PSCustomObject]@{
        interface_alias    = $SelectedAdapter.Alias
        mac_address        = $SelectedAdapter.MACAddress.Replace("-", ":").ToUpper()
        interface_index    = $SelectedAdapter.InterfaceIndex
        adapter_guid       = $targetGuid
        description        = $SelectedAdapter.Description
        ip_address         = $DisklessIP
        subnet_mask        = $SubnetMask
        prefix_length      = $prefixLen
        dhcp_range_start   = $DHCPStart
        dhcp_range_end     = $DHCPEnd
        lease_duration     = 86400
    }
    safety_policy = [PSCustomObject]@{
        require_independent_mgmt = $true
        prohibit_primary_internet = $true
        prohibit_wireless        = $true
        prohibit_virtual         = $true
        staging_applied          = $false
        pxe_active               = $false
    }
}

$utf8NoBOM = New-Object System.Text.UTF8Encoding($false)
$ProfilePath = Join-Path $ConfigDir "network_profile.json"
$ProfileJson = $Profile | ConvertTo-Json -Depth 5
[System.IO.File]::WriteAllText($ProfilePath, $ProfileJson, $utf8NoBOM)
Write-Host "Network profile saved: $ProfilePath" -ForegroundColor Green

# 5. Generate or Update Server Configuration
Write-Host ""
Write-Host "[4/5] Generating server configuration (config\nkb_config.json)..." -ForegroundColor Yellow

$NkbConfig = [PSCustomObject]@{
    app_version            = "1.0.0"
    server_role            = $ServerRole
    api_port               = 8443
    storage_root           = $StorageRoot
    database_dir           = (Join-Path $StorageRoot "database")
    logs_dir               = (Join-Path $StorageRoot "logs")
    tftp_dir               = (Join-Path $InstallDir "tftpboot")
    web_dir                = (Join-Path $InstallDir "web\dist")
    network_profile_path   = $ProfilePath
    enable_proxy_dhcp      = $false
    enable_tftp            = $false
    enable_http_boot       = $false
    http_boot_port         = 8080
    dhcp = [PSCustomObject]@{
        enabled            = $false
        listen_interface   = $SelectedAdapter.Alias
        server_ip          = $DisklessIP
        pool_start         = $DHCPStart
        pool_end           = $DHCPEnd
        subnet_mask        = $SubnetMask
        lease_seconds      = 86400
    }
    tftp = [PSCustomObject]@{
        enabled            = $false
        root_dir           = (Join-Path $InstallDir "tftpboot")
    }
    http_boot = [PSCustomObject]@{
        enabled            = $false
        port               = 8080
    }
}

$NkbConfigPath = Join-Path $ConfigDir "nkb_config.json"
$NkbConfigJson = $NkbConfig | ConvertTo-Json -Depth 5
[System.IO.File]::WriteAllText($NkbConfigPath, $NkbConfigJson, $utf8NoBOM)
Write-Host "Server config saved: $NkbConfigPath" -ForegroundColor Green

# 6. Safety Confirmation
Write-Host ""
Write-Host "[5/5] First-run configuration complete." -ForegroundColor Yellow
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "   FIRST-RUN SETUP SUCCESSFUL — SAFETY POLICY ACTIVE            " -ForegroundColor Green
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "NOTE: Network settings were NOT modified and PXE listeners remain STOPPED."
Write-Host "To stage the dedicated NIC and start PXE boot when physical wiring is ready:"
Write-Host "  1. Verify physical unmanaged switch isolation."
Write-Host "  2. Review 'config\scripts\stage_adapter.ps1'."
Write-Host "  3. Execute 'stage_adapter.ps1' with Administrator privileges."
Write-Host "=================================================================" -ForegroundColor Cyan
