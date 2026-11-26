# Network Safety and ICS Audit Script
$ErrorActionPreference = "Continue"

Write-Host "--- IP FORWARDING AUDIT ---"
Get-NetIPInterface | Where-Object { $_.Forwarding -eq 'Enabled' } | Select-Object ifIndex, InterfaceAlias, AddressFamily

Write-Host "--- FIREWALL PROFILE STATUS ---"
Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction, DefaultOutboundAction

Write-Host "--- ADAPTER BRIDGES ---"
Get-NetAdapterBinding | Where-Object { $_.ComponentId -eq 'ms_bridge' -and $_.Enabled -eq $true }

Write-Host "--- PHYSICAL ADAPTER STATUS ---"
Get-NetAdapter | Where-Object { $_.HardwareInterface -eq $true } | Select-Object Name, InterfaceDescription, ifIndex, Status, MacAddress, LinkSpeed
