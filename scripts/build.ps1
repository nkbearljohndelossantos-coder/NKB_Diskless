# NKB Diskless System - Comprehensive Build Script
# Builds Go Server, Workstation Agent, React Frontend, and Wails Desktop Manager

$ErrorActionPreference = "Stop"
$env:PATH = "$env:LOCALAPPDATA\Programs\go\bin;$env:PATH"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " NKB DISKLESS SYSTEM - COMPREHENSIVE PROJECT BUILD" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# Ensure bin directory exists
if (-not (Test-Path "bin")) {
    New-Item -ItemType Directory -Path "bin" | Out-Null
}

# 1. Build React Web Frontend
Write-Host "`n[1/4] Building React 19 Frontend with Vite..." -ForegroundColor Yellow
cmd.exe /c "cd frontend && npm run build"
if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
Write-Host "✓ React Frontend built to frontend/dist" -ForegroundColor Green

# 2. Build Go Core Server
Write-Host "`n[2/4] Compiling Go Server (bin\nkb-server.exe)..." -ForegroundColor Yellow
go build -o bin\nkb-server.exe cmd\server\main.go
if ($LASTEXITCODE -ne 0) { throw "Server compilation failed" }
Write-Host "✓ Server compiled: bin\nkb-server.exe" -ForegroundColor Green

# 3. Build Workstation Background Agent
Write-Host "`n[3/4] Compiling Workstation Agent (bin\nkb-agent.exe)..." -ForegroundColor Yellow
go build -o bin\nkb-agent.exe cmd\agent\main.go
if ($LASTEXITCODE -ne 0) { throw "Agent compilation failed" }
Write-Host "✓ Agent compiled: bin\nkb-agent.exe" -ForegroundColor Green

# 4. Build Wails Desktop Manager
Write-Host "`n[4/4] Compiling Wails Desktop Manager (bin\nkb-desktop-manager.exe)..." -ForegroundColor Yellow
Copy-Item -Recurse -Force frontend\dist desktop\dist
Push-Location desktop
go build -o ..\bin\nkb-desktop-manager.exe .
Pop-Location
if ($LASTEXITCODE -ne 0) { throw "Desktop Manager compilation failed" }
Write-Host "✓ Desktop Manager compiled: bin\nkb-desktop-manager.exe" -ForegroundColor Green

Write-Host "`n==========================================================" -ForegroundColor Cyan
Write-Host " ALL NKB PLATFORM BINARIES COMPILED SUCCESSFULLY!" -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Cyan
Get-ChildItem bin\*.exe | Select-Object Name, Length, LastWriteTime | Format-Table -AutoSize
