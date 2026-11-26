<#
.SYNOPSIS
    Stops and removes the NKB Diskless System Server Windows Service.
#>

[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "SilentlyContinue"

$ServiceName = "NKBDisklessServer"

Write-Host "Stopping and removing Windows Service: $ServiceName..." -ForegroundColor Cyan

$svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($svc) {
    if ($svc.Status -eq "Running") {
        Write-Host "Stopping $ServiceName..."
        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2
    }
    Write-Host "Deleting $ServiceName..."
    & sc.exe delete $ServiceName | Out-Null
    Write-Host "Service $ServiceName removed." -ForegroundColor Green
} else {
    Write-Host "Service $ServiceName does not exist." -ForegroundColor Yellow
}
