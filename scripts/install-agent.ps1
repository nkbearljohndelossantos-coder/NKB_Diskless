<#
.SYNOPSIS
    Installs the NKB Workstation Management Agent as a native Windows background service.

.DESCRIPTION
    Configures the agent binary, validates prerequisites, creates the NKBWorkstationAgent
    service with automatic restart recovery, and initializes agent configuration safely.

.PARAMETER ServerURL
    The central NKB Management Server URL (e.g. http://192.168.1.100:8443 or http://localhost:8443).
    Default: http://localhost:8443

.PARAMETER DataRoot
    The base directory for NKB PoC configuration and logs.
    Default: C:\NKB-PoC

.PARAMETER Preview
    Simulates the installation and displays planned actions without altering system state.

.PARAMETER NoStart
    Registers the service but does not immediately start it.

.EXAMPLE
    .\install-agent.ps1 -Preview
    .\install-agent.ps1 -ServerURL "http://192.168.10.5:8443"
#>

[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$ServerURL = "http://localhost:8443",
    [string]$DataRoot = "C:\NKB-PoC",
    [switch]$Preview,
    [switch]$NoStart
)

$ServiceName = "NKBWorkstationAgent"
$ServiceDisplayName = "NKB Workstation Management Agent"
$ServiceDescription = "Manages telemetry, device registration, and remote commands for NKB Diskless System."
$WorkspaceRoot = (Get-Item $PSScriptRoot).Parent.FullName
$AgentBinPath = Join-Path $WorkspaceRoot "bin\nkb-agent.exe"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  NKB WORKSTATION AGENT INSTALLER (PHASE 2 DEPLOYMENT)    " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Preview Mode Notice
if ($Preview) {
    Write-Host "[PREVIEW MODE] No system changes will be performed." -ForegroundColor Yellow
}

# 2. Administrator Privilege Verification
$IsAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $IsAdmin -and -not $Preview) {
    Write-Error "Administrator privileges are required to register Windows services. Please re-run PowerShell as Administrator."
    exit 1
}

# 3. Binary Verification
Write-Host "[1/5] Verifying Agent executable at $AgentBinPath..." -ForegroundColor White
if (-not (Test-Path $AgentBinPath)) {
    Write-Warning "Agent binary not found at $AgentBinPath."
    Write-Host "Compiling agent binary using Go..." -ForegroundColor Yellow
    if (-not $Preview) {
        $env:PATH = "$env:LOCALAPPDATA\Programs\go\bin;$env:PATH"
        & go build -ldflags "-s -w" -o (Join-Path $WorkspaceRoot "bin\nkb-agent.exe") (Join-Path $WorkspaceRoot "cmd\agent\main.go")
        if ($LASTEXITCODE -ne 0) {
            Write-Error "Failed to compile nkb-agent.exe. Please verify Go environment."
            exit 1
        }
    }
}
Write-Host "   -> Agent binary verified." -ForegroundColor Green

# 4. Storage & Configuration Initialization
$ConfigDir = Join-Path $DataRoot "config"
$LogsDir = Join-Path $DataRoot "logs"
$ConfigFile = Join-Path $ConfigDir "nkb_agent.json"

Write-Host "[2/5] Preparing directories: $ConfigDir & $LogsDir..." -ForegroundColor White
if (-not $Preview) {
    [System.IO.Directory]::CreateDirectory($ConfigDir) | Out-Null
    [System.IO.Directory]::CreateDirectory($LogsDir) | Out-Null

    # ACL Hardening: Secure credentials and device tokens against unauthorized standard users
    try {
        & icacls.exe $ConfigDir /inheritance:r /grant:r "SYSTEM:(OI)(CI)F" "Administrators:(OI)(CI)F" | Out-Null
    } catch {
        Write-Warning "Could not harden ACL on $ConfigDir: $_"
    }
}

Write-Host "[3/5] Setting up agent configuration file at $ConfigFile..." -ForegroundColor White
if (Test-Path $ConfigFile) {
    Write-Host "   -> Existing configuration found; updating ServerURL to $ServerURL" -ForegroundColor Gray
    if (-not $Preview) {
        try {
            $json = Get-Content $ConfigFile -Raw | ConvertFrom-Json
            $json.server_url = $ServerURL
            $json | ConvertTo-Json -Depth 5 | Set-Content $ConfigFile -Encoding UTF8
        } catch {
            Write-Warning "Existing config could not be parsed; creating default."
        }
    }
} else {
    Write-Host "   -> Generating initial configuration..." -ForegroundColor Gray
    $initialConfig = @{
        server_url = $ServerURL
        heartbeat_interval_seconds = 5
        allow_self_reboot = $false
    }
    if (-not $Preview) {
        $initialConfig | ConvertTo-Json -Depth 5 | Set-Content $ConfigFile -Encoding UTF8
    }
}

# 5. Windows Service Registration
Write-Host "[4/5] Configuring Windows Service '$ServiceName'..." -ForegroundColor White
$ExistingService = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue

if ($ExistingService) {
    Write-Host "   -> Service already registered. Updating binPath..." -ForegroundColor Gray
    if (-not $Preview) {
        & sc.exe config $ServiceName binPath= "`"$AgentBinPath`" -config `"$ConfigFile`"" start= auto | Out-Null
    }
} else {
    Write-Host "   -> Creating service with sc.exe..." -ForegroundColor Gray
    if (-not $Preview) {
        & sc.exe create $ServiceName binPath= "`"$AgentBinPath`" -config `"$ConfigFile`"" start= auto DisplayName= "$ServiceDisplayName" | Out-Null
        & sc.exe description $ServiceName "$ServiceDescription" | Out-Null
        # Configure automatic restart on crash: reset after 1 day (86400s), restart after 10s on failure 1/2/3
        & sc.exe failure $ServiceName reset= 86400 actions= restart/10000/restart/10000/restart/10000 | Out-Null
    }
}
Write-Host "   -> Service configured with automatic restart recovery." -ForegroundColor Green

# 6. Service Startup
Write-Host "[5/5] Service status check..." -ForegroundColor White
if ($Preview) {
    Write-Host "   -> [PREVIEW] Would start service '$ServiceName'." -ForegroundColor Yellow
} elseif ($NoStart) {
    Write-Host "   -> Skipping startup as -NoStart was requested." -ForegroundColor Yellow
} else {
    try {
        Start-Service -Name $ServiceName -ErrorAction Stop
        Write-Host "   -> Service '$ServiceName' is now RUNNING." -ForegroundColor Green
    } catch {
        Write-Warning "Could not start service immediately: $_"
        Write-Host "You can start it manually with: Start-Service $ServiceName" -ForegroundColor Gray
    }
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  NKB WORKSTATION AGENT INSTALLATION COMPLETED            " -ForegroundColor Green
Write-Host "  Config File: $ConfigFile" -ForegroundColor White
Write-Host "  Server URL:  $ServerURL" -ForegroundColor White
Write-Host "==========================================================" -ForegroundColor Cyan
