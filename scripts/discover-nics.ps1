# NKB Diskless System - Read-Only Network Interface Discovery & Safety Classifier
# Phase 3B.0 Physical Preparation Script

[CmdletBinding()]
param()

$ErrorActionPreference = "SilentlyContinue"

Write-Host "================================================================================" -ForegroundColor Cyan
Write-Host " NKB DISKLESS SYSTEM — NETWORK INTERFACE DISCOVERY & SAFETY CLASSIFIER" -ForegroundColor Cyan
Write-Host " Mode: READ-ONLY AUDIT (Zero configuration modifications)" -ForegroundColor Cyan
Write-Host "================================================================================" -ForegroundColor Cyan

$adapters = @{}
Get-NetAdapter | ForEach-Object {
    $adapters[$_.InterfaceAlias] = @{
        Status = $_.Status
        LinkSpeed = $_.LinkSpeed
        PhysicalMediaType = $_.PhysicalMediaType
        MacAddress = $_.MacAddress
        DeviceID = $_.DeviceID
    }
}

$configs = Get-NetIPConfiguration

$report = foreach ($cfg in $configs) {
    $alias = $cfg.InterfaceAlias
    $meta = $adapters[$alias]
    $ipv4 = ($cfg.IPv4Address.IPAddress -join ', ')
    $gw = ($cfg.IPv4DefaultGateway.NextHop -join ', ')
    $status = if ($meta) { $meta.Status } else { 'Unknown' }
    $speed = if ($meta) { $meta.LinkSpeed } else { 'Unknown' }
    $mac = if ($meta) { $meta.MacAddress } else { 'Unknown' }
    $media = if ($meta) { $meta.PhysicalMediaType } else { 'Unknown' }
    $desc = $cfg.InterfaceDescription

    $isPhysical = $true
    if ($desc -like "*Virtual*" -or $desc -like "*Hyper-V*" -or $alias -like "*vEthernet*" -or $desc -like "*Loopback*") {
        $isPhysical = $false
    }

    $classification = "UNCLASSIFIED"
    $safetyAction = "Review before binding"
    $color = "Yellow"

    if ($ipv4 -like "*192.168.50.*") {
        $classification = "TARGET TEST NIC"
        $safetyAction = "APPROVED FOR PHASE 3B STAGING"
        $color = "Green"
        if ($gw) {
            $safetyAction = "WARNING: Remove Default Gateway before testing"
            $color = "Red"
        }
    } elseif ($gw -or ($ipv4 -and $ipv4 -notlike "169.254.*")) {
        $classification = "PROTECTED PRODUCTION NIC"
        $safetyAction = "STRICTLY PROHIBITED FROM PXE BINDING"
        $color = "Red"
    } elseif ($status -eq "Disconnected") {
        $classification = "DISCONNECTED"
        $safetyAction = "Cable unplugged or inactive"
        $color = "DarkGray"
    }

    [PSCustomObject]@{
        Name           = $alias
        Status         = $status
        IPv4           = if ($ipv4) { $ipv4 } else { "None" }
        Gateway        = if ($gw) { $gw } else { "None" }
        MAC            = $mac
        Type           = if ($isPhysical) { "Physical" } else { "Virtual" }
        Classification = $classification
        SafetyAction   = $safetyAction
        Description    = $desc
    }
}

$report | Format-Table -Property Name, Status, IPv4, Gateway, MAC, Type, Classification, SafetyAction -AutoSize

Write-Host "================================================================================" -ForegroundColor Cyan
Write-Host " SAFETY SUMMARY & BINDING ADVISORY:" -ForegroundColor Cyan
Write-Host "================================================================================" -ForegroundColor Cyan

$testNIC = $report | Where-Object { $_.Classification -eq "TARGET TEST NIC" }
if ($testNIC) {
    Write-Host "[✓] Dedicated Test NIC Identified: $($testNIC.Name) ($($testNIC.IPv4))" -ForegroundColor Green
} else {
    Write-Host "[!] NO DEDICATED TEST NIC (192.168.50.X) FOUND ON SYSTEM!" -ForegroundColor Yellow
    Write-Host "    To prepare for Phase 3B physical testing:" -ForegroundColor Yellow
    Write-Host "    1. Attach secondary physical Gigabit Ethernet adapter (e.g. USB 3.0-to-GbE dongle)." -ForegroundColor Yellow
    Write-Host "    2. Assign static IP 192.168.50.1 / 255.255.255.0 with NO default gateway." -ForegroundColor Yellow
    Write-Host "    3. Connect patch cable directly to isolated test switch." -ForegroundColor Yellow
}

$protectedNICs = $report | Where-Object { $_.Classification -eq "PROTECTED PRODUCTION NIC" }
Write-Host "`n[i] Protected Production Adapters (Guaranteed safe; network boot binding prohibited):" -ForegroundColor White
foreach ($p in $protectedNICs) {
    Write-Host "    - $($p.Name): $($p.IPv4) (GW: $($p.Gateway)) [$($p.Description)]" -ForegroundColor Gray
}

Write-Host "================================================================================`n" -ForegroundColor Cyan
