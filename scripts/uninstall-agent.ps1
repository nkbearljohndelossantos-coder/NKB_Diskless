<#
.SYNOPSIS
    Uninstalls the NKB Workstation Management Agent Windows service.

.DESCRIPTION
    Safely stops and removes the NKBWorkstationAgent Windows service.
    Configuration and pending result files are preserved by default for safety.

.PARAMETER PurgeData
    If specified, removes local configuration and buffer files in C:\NKB-PoC\config\.

.PARAMETER Preview
    Simulates the uninstallation without stopping or deleting the service.

.EXAMPLE
    .\uninstall-agent.ps1 -Preview
    .\uninstall-agent.ps1
    .\uninstall-agent.ps1 -PurgeData
#>

[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$DataRoot = "C:\NKB-PoC",
    [switch]$PurgeData,
    [switch]$Preview
)

$ServiceName = "NKBWorkstationAgent"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  NKB WORKSTATION AGENT UNINSTALLER                       " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

if ($Preview) {
    Write-Host "[PREVIEW MODE] No changes will be made to system services." -ForegroundColor Yellow
}

# 1. Administrator Check
$IsAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $IsAdmin -and -not $Preview) {
    Write-Error "Administrator privileges are required to remove Windows services. Please re-run PowerShell as Administrator."
    exit 1
}

# 2. Check Service Existence
$svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $svc) {
    Write-Host "Service '$ServiceName' is not installed." -ForegroundColor Yellow
} else {
    # 3. Stop Service
    if ($svc.Status -eq 'Running') {
        Write-Host "Stopping service '$ServiceName'..." -ForegroundColor White
        if (-not $Preview) {
            Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
            Start-Sleep -Seconds 1
        }
    }

    # 4. Delete Service
    Write-Host "Deleting service '$ServiceName' via sc.exe..." -ForegroundColor White
    if (-not $Preview) {
        & sc.exe delete $ServiceName | Out-Null
        Write-Host "Service '$ServiceName' removed." -ForegroundColor Green
    } else {
        Write-Host "[PREVIEW] Would execute: sc.exe delete $ServiceName" -ForegroundColor Yellow
    }
}

# 5. Handle Data Files
$ConfigFile = Join-Path $DataRoot "config\nkb_agent.json"
$BufferFile = Join-Path $DataRoot "config\pending_results.json"

if ($PurgeData) {
    Write-Host "Purging local agent configuration and result buffers..." -ForegroundColor White
    if (-not $Preview) {
        if (Test-Path $ConfigFile) { Remove-Item $ConfigFile -Force }
        if (Test-Path $BufferFile) { Remove-Item $BufferFile -Force }
        Write-Host "Agent data files purged." -ForegroundColor Green
    } else {
        Write-Host "[PREVIEW] Would delete $ConfigFile and $BufferFile" -ForegroundColor Yellow
    }
} else {
    Write-Host "Preserved agent configuration ($ConfigFile) and offline buffer." -ForegroundColor Gray
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  UNINSTALLATION COMPLETED                                " -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Cyan
