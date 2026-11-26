<#
.SYNOPSIS
    Registers NKB Diskless System Server as a Windows Service.
.DESCRIPTION
    Registers nkb-server.exe as 'NKBDisklessServer' with Manual startup type.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$InstallDir = $PSScriptRoot,

    [Parameter(Mandatory = $false)]
    [string]$ConfigPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ServiceName = "NKBDisklessServer"
$DisplayName = "NKB Diskless System Server"
$Description = "High-Performance Diskless Boot, Storage & Fleet Management Service"

$ServerExe = Join-Path $InstallDir "bin\nkb-server.exe"
if (-not (Test-Path $ServerExe)) {
    # If called from installer-source or scripts subdirectory
    $altExe = Join-Path (Split-Path $InstallDir -Parent) "bin\nkb-server.exe"
    if (Test-Path $altExe) {
        $ServerExe = $altExe
        $InstallDir = Split-Path $InstallDir -Parent
    } else {
        throw "Cannot find nkb-server.exe at $ServerExe"
    }
}

if (-not $ConfigPath) {
    $ConfigPath = Join-Path $InstallDir "config\nkb_config.json"
}

$BinaryPath = ('"{0}" -config "{1}"' -f $ServerExe, $ConfigPath)

Write-Host "Registering Windows Service: $ServiceName..." -ForegroundColor Cyan
Write-Host "Binary Path: $BinaryPath"

$Existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($Existing) {
    Write-Host "Service $ServiceName already exists. Updating binary path..." -ForegroundColor Yellow
    & sc.exe config $ServiceName binPath= "$BinaryPath" start= demand | Out-Null
} else {
    New-Service -Name $ServiceName `
                -DisplayName $DisplayName `
                -Description $Description `
                -BinaryPathName $BinaryPath `
                -StartupType Manual | Out-Null
}

Write-Host "Service '$ServiceName' registered successfully with Manual startup type." -ForegroundColor Green
Write-Host "NOTE: The service is intentionally set to Manual and is NOT started automatically."
