<#
.SYNOPSIS
    Automated Deployment & Infrastructure Setup Script for NKB Diskless System.
.DESCRIPTION
    Validates server prerequisites, configures Windows Firewall rules for PXE/TFTP/iSCSI,
    prepares directory structures, and installs the NKB Server background daemon.
.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\scripts\install-nkb-server.ps1
#>

[CmdletBinding()]
param (
    [string]$InstallDir = "C:\NKB_Diskless",
    [int]$APIPort = 8443,
    [int]$ISCSIPort = 3260,
    [switch]$EnableWindowsISCSITarget,
    [switch]$InstallWindowsService
)

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "   NKB DISKLESS SYSTEM - AUTOMATED DEPLOYMENT SCRIPT     " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Check Administrator Privileges
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Warning "This script requires Administrator privileges for Firewall & Service configuration."
    Write-Warning "Please relaunch PowerShell as Administrator."
}

# 2. Check Hardware Resources
Write-Host "`n[1/5] Evaluating Host Hardware Capacity..." -ForegroundColor Yellow
$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$ram = Get-CimInstance Win32_OperatingSystem
$totalRAMGB = [math]::Round($ram.TotalVisibleMemorySize / 1MB, 2)
$freeRAMGB  = [math]::Round($ram.FreePhysicalMemory / 1MB, 2)

Write-Host "  CPU: $($cpu.Name) ($($cpu.NumberOfCores) Cores, $($cpu.NumberOfLogicalProcessors) Threads)"
Write-Host "  RAM: $totalRAMGB GB Total ($freeRAMGB GB Available)"

if ($totalRAMGB -lt 15) {
    Write-Warning "Detected under 16GB RAM ($totalRAMGB GB). For more than 5 clients, 32GB+ is recommended."
} else {
    Write-Host "  RAM Capacity: Verified for baseline deployment (5-10 clients)." -ForegroundColor Green
}

# 3. Create Storage Tree
Write-Host "`n[2/5] Initializing Directory Hierarchy in $InstallDir..." -ForegroundColor Yellow
$folders = @("disks", "cache", "snapshots", "tftpboot\uefi", "tftpboot\bios", "data", "backups", "logs")
foreach ($f in $folders) {
    $target = Join-Path $InstallDir $f
    if (-not (Test-Path $target)) {
        New-Item -ItemType Directory -Path $target -Force | Out-Null
        Write-Host "  Created: $target"
    }
}

# 4. Configure Windows Firewall
Write-Host "`n[3/5] Configuring Windows Advanced Firewall Rules..." -ForegroundColor Yellow
$rules = @(
    @{ Name = "NKB-TFTP-UDP69"; Protocol = "UDP"; Port = 69; Desc = "NKB PXE Boot TFTP" },
    @{ Name = "NKB-ProxyDHCP-UDP4011"; Protocol = "UDP"; Port = 4011; Desc = "NKB PXE ProxyDHCP" },
    @{ Name = "NKB-API-TCP8443"; Protocol = "TCP"; Port = $APIPort; Desc = "NKB Admin Dashboard & REST API" },
    @{ Name = "NKB-iSCSI-TCP3260"; Protocol = "TCP"; Port = $ISCSIPort; Desc = "NKB Virtual Block SAN Target" }
)

foreach ($r in $rules) {
    $existing = Get-NetFirewallRule -Name $r.Name -ErrorAction SilentlyContinue
    if (-not $existing) {
        New-NetFirewallRule -Name $r.Name -DisplayName $r.Desc -Direction Inbound -Protocol $r.Protocol -LocalPort $r.Port -Action Allow | Out-Null
        Write-Host "  Firewall rule created: $($r.Name) ($($r.Protocol) $($r.Port))" -ForegroundColor Green
    } else {
        Write-Host "  Firewall rule exists: $($r.Name)" -ForegroundColor Gray
    }
}

# 5. Optional Windows iSCSI Target Server Feature
if ($EnableWindowsISCSITarget) {
    Write-Host "`n[4/5] Enabling Windows iSCSI Target Feature..." -ForegroundColor Yellow
    if (Get-Command Install-WindowsFeature -ErrorAction SilentlyContinue) {
        Install-WindowsFeature -Name FS-iSCSITarget-Server -IncludeManagementTools
    } elseif (Get-Command Enable-WindowsOptionalFeature -ErrorAction SilentlyContinue) {
        Enable-WindowsOptionalFeature -Online -FeatureName "iSCSITarget" -All -NoRestart
    }
}

# 6. Windows Service Registration
if ($InstallWindowsService) {
    Write-Host "`n[5/5] Registering NKB Server as Windows Background Service..." -ForegroundColor Yellow
    $binPath = Join-Path $InstallDir "bin\nkb-server.exe"
    if (Test-Path $binPath) {
        $svc = Get-Service -Name "NKBDiskless" -ErrorAction SilentlyContinue
        if (-not $svc) {
            New-Service -Name "NKBDiskless" -DisplayName "NKB Diskless System Service" -BinaryPathName "$binPath -data $InstallDir" -StartupType Automatic | Out-Null
            Write-Host "  Service 'NKBDiskless' registered successfully." -ForegroundColor Green
        }
    } else {
        Write-Warning "Binary not found at $binPath. Copy nkb-server.exe to bin\ before starting service."
    }
}

Write-Host "`n==========================================================" -ForegroundColor Green
Write-Host "   NKB DISKLESS SYSTEM INITIALIZATION COMPLETE           " -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Green
Write-Host "Dashboard URL: http://localhost:$APIPort"
Write-Host "Configuration: $InstallDir\nkb_config.json"
