<#
.SYNOPSIS
    Configures Windows Defender Firewall rules for NKB Diskless System.
.DESCRIPTION
    Creates or removes inbound firewall rules for DHCP, ProxyDHCP, TFTP, HTTP Boot, API, and iSCSI.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [ValidateSet("Enable", "Remove", "Disable")]
    [string]$Action = "Enable"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$GroupName = "NKB Diskless System"

if ($Action -in @("Remove", "Disable")) {
    Write-Host "Removing firewall rules for group '$GroupName'..." -ForegroundColor Cyan
    Remove-NetFirewallRule -Group $GroupName -ErrorAction SilentlyContinue
    Write-Host "Firewall rules removed." -ForegroundColor Green
    return
}

Write-Host "Configuring Windows Defender Firewall rules for group '$GroupName'..." -ForegroundColor Cyan

# Remove old rules first to ensure clean state
Remove-NetFirewallRule -Group $GroupName -ErrorAction SilentlyContinue

$Rules = @(
    @{ Name = "NKB-DHCP-In-UDP"; DisplayName = "NKB Diskless - DHCP Server (UDP 67)"; Protocol = "UDP"; Port = 67; Dir = "Inbound" },
    @{ Name = "NKB-ProxyDHCP-In-UDP"; DisplayName = "NKB Diskless - ProxyDHCP (UDP 4011)"; Protocol = "UDP"; Port = 4011; Dir = "Inbound" },
    @{ Name = "NKB-TFTP-In-UDP"; DisplayName = "NKB Diskless - TFTP Boot (UDP 69)"; Protocol = "UDP"; Port = 69; Dir = "Inbound" },
    @{ Name = "NKB-HTTPBoot-In-TCP"; DisplayName = "NKB Diskless - HTTP iPXE Boot (TCP 8080)"; Protocol = "TCP"; Port = 8080; Dir = "Inbound" },
    @{ Name = "NKB-API-In-TCP"; DisplayName = "NKB Diskless - Management Web API (TCP 8443)"; Protocol = "TCP"; Port = 8443; Dir = "Inbound" },
    @{ Name = "NKB-iSCSI-In-TCP"; DisplayName = "NKB Diskless - iSCSI Target (TCP 3260)"; Protocol = "TCP"; Port = 3260; Dir = "Inbound" }
)

foreach ($r in $Rules) {
    New-NetFirewallRule -Name $r.Name `
                        -DisplayName $r.DisplayName `
                        -Group $GroupName `
                        -Protocol $r.Protocol `
                        -LocalPort $r.Port `
                        -Direction $r.Dir `
                        -Action Allow `
                        -Profile Any `
                        -Enabled True | Out-Null
    Write-Host ("  + Created rule: {0} ({1} {2})" -f $r.DisplayName, $r.Protocol, $r.Port) -ForegroundColor Green
}

Write-Host "Firewall configuration completed successfully." -ForegroundColor Green
