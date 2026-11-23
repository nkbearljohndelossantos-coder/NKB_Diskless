# NKB Diskless System - Development Server Launch Script
# Starts the backend HTTP/REST server pointing to safe storage directory C:\NKB-PoC

param (
    [string]$DataDir = "C:\NKB-PoC",
    [int]$Port = 8080
)

$ErrorActionPreference = "Stop"
$env:PATH = "$env:LOCALAPPDATA\Programs\go\bin;$env:PATH"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " LAUNCHING NKB DISKLESS DEVELOPMENT SERVER" -ForegroundColor Cyan
Write-Host " Data Directory: $DataDir" -ForegroundColor Yellow
Write-Host " Port:           $Port" -ForegroundColor Yellow
Write-Host "==========================================================" -ForegroundColor Cyan

# Launch server
& ".\bin\nkb-server.exe" -data $DataDir -port $Port
