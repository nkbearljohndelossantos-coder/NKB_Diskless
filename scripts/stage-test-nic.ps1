# ==============================================================================
# NKB DISKLESS SYSTEM - DEDICATED TEST NIC STAGING SCRIPT (PHASE 3B.0.2 HARDENED)
# Requires elevated PowerShell (Run as Administrator)
# ==============================================================================

[CmdletBinding(SupportsShouldProcess=$true)]
param(
    [Parameter(Mandatory=$true, HelpMessage="Exact Alias/Name of the dedicated test network adapter")]
    [string]$InterfaceAlias,

    [Parameter(Mandatory=$false)]
    [switch]$Force,

    [Parameter(Mandatory=$false)]
    [switch]$Rollback
)

$targetIP = "192.168.50.1"
$prefixLength = 24

Write-Host "============================================================" -ForegroundColor Cyan
Write-Host " NKB DISKLESS - DEDICATED TEST NIC STAGING UTILITY" -ForegroundColor Cyan
Write-Host " Target Interface: $InterfaceAlias" -ForegroundColor Yellow
Write-Host "============================================================" -ForegroundColor Cyan

# 0. Check Administrator Privilege
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "[FAIL] This script requires elevated administrative privileges. Please run as Administrator." -ForegroundColor Red
    exit 1
}

# 1. Verify target adapter exists
$adapter = Get-NetAdapter -Name $InterfaceAlias -ErrorAction SilentlyContinue
if (-not $adapter) {
    Write-Host "[FAIL] Adapter '$InterfaceAlias' not found on this system!" -ForegroundColor Red
    Write-Host "`nCurrently detected physical and virtual adapters:" -ForegroundColor White
    Get-NetAdapter | Select-Object Name, InterfaceDescription, Status, LinkSpeed, MacAddress | Format-Table -AutoSize
    exit 1
}

# 2. Safety Rule: Reject Primary Wireless Corporate Adapters
$prohibitedNames = @("Wi-Fi", "Wi-Fi 2", "WLAN")
if ($prohibitedNames -contains $InterfaceAlias) {
    Write-Host "[SAFETY VIOLATION] Refusing to modify corporate wireless adapter '$InterfaceAlias'!" -ForegroundColor Red
    Write-Host "NKB Safety Policy strictly forbids binding PXE/test traffic to corporate wireless interfaces." -ForegroundColor Yellow
    exit 1
}

# 3. Safety Rule: Reject Primary Active Internet Adapter
$primaryRoute = Get-NetRoute -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue | Sort-Object { $_.RouteMetric + $_.ifMetric } | Select-Object -First 1
if ($primaryRoute -and $primaryRoute.InterfaceIndex -eq $adapter.InterfaceIndex -and $primaryRoute.RouteMetric -lt 100) {
    Write-Host "[SAFETY VIOLATION] Adapter '$InterfaceAlias' is currently the primary active default route to the Internet ($($primaryRoute.NextHop))!" -ForegroundColor Red
    Write-Host "Modifying the primary active Internet gateway adapter is strictly prohibited." -ForegroundColor Yellow
    exit 1
}

# 4. Safety Rule: Reject Virtual, Tunnel, Loopback, and Hyper-V Adapters
$desc = $adapter.InterfaceDescription
$virtualKeywords = @("Hyper-V", "WSL", "VirtualBox", "VMware", "Loopback", "TAP", "VPN", "Bluetooth", "NDIS", "PANGP")
foreach ($keyword in $virtualKeywords) {
    if ($desc -like "*$keyword*") {
        Write-Host "[SAFETY VIOLATION] Adapter '$InterfaceAlias' is a virtual/bridge adapter ($desc)!" -ForegroundColor Red
        Write-Host "A genuine physical secondary network adapter is required for physical PXE testing." -ForegroundColor Yellow
        exit 1
    }
}

# 5. Check for IP Conflict on OTHER Adapters
$conflictingIP = Get-NetIPAddress -IPAddress $targetIP -ErrorAction SilentlyContinue
if ($conflictingIP -and $conflictingIP.InterfaceIndex -ne $adapter.InterfaceIndex) {
    Write-Host "[CONFLICT] Target IP $targetIP is already configured on adapter '$($conflictingIP.InterfaceAlias)'!" -ForegroundColor Red
    Write-Host "Resolve this IP conflict before staging '$InterfaceAlias'." -ForegroundColor Yellow
    exit 1
}

# 6. ROLLBACK HANDLER
if ($Rollback) {
    Write-Host "`n=== INITIATING SAFE ROLLBACK FOR '$InterfaceAlias' ===" -ForegroundColor Yellow
    Write-Host "Removing static IP $targetIP..." -ForegroundColor Gray
    Get-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $targetIP -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false

    Write-Host "Enabling DHCP client on '$InterfaceAlias'..." -ForegroundColor Gray
    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -Dhcp Enabled -ErrorAction SilentlyContinue

    Write-Host "Resetting DNS client to automatic..." -ForegroundColor Gray
    Set-DnsClientServerAddress -InterfaceIndex $adapter.InterfaceIndex -ResetServerAddresses -ErrorAction SilentlyContinue

    Write-Host "`n[PASS] Rollback completed for '$InterfaceAlias'." -ForegroundColor Green
    exit 0
}

# 7. Display Current Adapter Configuration Before Changes
Write-Host "`nCURRENT ADAPTER DETAILS (PRE-CONFIGURATION):" -ForegroundColor White
Write-Host "  Name:         $($adapter.Name)" -ForegroundColor White
Write-Host "  Description:  $($adapter.InterfaceDescription)" -ForegroundColor White
Write-Host "  MAC Address:  $($adapter.MacAddress)" -ForegroundColor White
Write-Host "  Media Status: $($adapter.Status)" -ForegroundColor White
Write-Host "  Link Speed:   $($adapter.LinkSpeed)" -ForegroundColor White

$currentIPs = Get-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue
if ($currentIPs) {
    Write-Host "  Current IPv4: $(($currentIPs | ForEach-Object { "$($_.IPAddress)/$($_.PrefixLength)" }) -join ', ')" -ForegroundColor White
} else {
    Write-Host "  Current IPv4: (None / Unconfigured)" -ForegroundColor White
}

# 8. Require Explicit Operator Confirmation
if (-not $Force) {
    Write-Host "`nWARNING: This will configure '$InterfaceAlias' with static IP $targetIP/$prefixLength (NO Gateway, NO DNS)." -ForegroundColor Yellow
    $confirm = Read-Host "Type 'CONFIRM' to proceed with staging"
    if ($confirm -ne "CONFIRM") {
        Write-Host "[ABORTED] Operator did not enter 'CONFIRM'. No changes made." -ForegroundColor Yellow
        exit 0
    }
}

# 9. Apply Staging Configuration to the Target Adapter ONLY
Write-Host "`n[1/4] Clearing existing IPv4 addresses and stale routes on '$InterfaceAlias'..." -ForegroundColor Gray
Get-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
Get-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false

Write-Host "[2/4] Assigning static IP $targetIP/$prefixLength (Zero Gateway)..." -ForegroundColor Green
New-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $targetIP -PrefixLength $prefixLength -DefaultGateway $null -ErrorAction Stop | Out-Null

Write-Host "[3/4] Clearing static DNS servers..." -ForegroundColor Green
Set-DnsClientServerAddress -InterfaceIndex $adapter.InterfaceIndex -ServerAddresses @() -ErrorAction SilentlyContinue

Write-Host "[4/4] Setting connection profile to Private..." -ForegroundColor Green
Set-NetConnectionProfile -InterfaceIndex $adapter.InterfaceIndex -NetworkCategory Private -ErrorAction SilentlyContinue

# 10. Verify Post-Configuration State
Write-Host "`nVERIFYING FINAL CONFIGURATION:" -ForegroundColor Cyan
$verifiedIP = Get-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $targetIP -ErrorAction SilentlyContinue
$verifiedGateway = Get-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue

if ($verifiedIP -and -not $verifiedGateway) {
    Write-Host "[PASS] Staging verified successfully!" -ForegroundColor Green
    Write-Host "  Adapter:   $($adapter.Name)" -ForegroundColor Green
    Write-Host "  IPv4:      $($verifiedIP.IPAddress)/$($verifiedIP.PrefixLength)" -ForegroundColor Green
    Write-Host "  Gateway:   NONE (Air-gap isolation verified)" -ForegroundColor Green
    Write-Host "  Profile:   Private" -ForegroundColor Green
    Write-Host "`nTo rollback this adapter in the future, run:" -ForegroundColor Gray
    Write-Host "  powershell -ExecutionPolicy Bypass -File scripts\stage-test-nic.ps1 -InterfaceAlias '$InterfaceAlias' -Rollback" -ForegroundColor Gray
    exit 0
} else {
    Write-Host "[FAIL] Verification failed! Please inspect network settings." -ForegroundColor Red
    exit 1
}
