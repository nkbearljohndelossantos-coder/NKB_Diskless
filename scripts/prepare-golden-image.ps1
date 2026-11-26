<#
.SYNOPSIS
    Golden Image Preparation & Sysprep Script for NKB Diskless System.
.DESCRIPTION
    Automates creation, mounting, OS staging, and network driver injection
    for diskless Windows 10/11 master images.
.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\scripts\prepare-golden-image.ps1 -ImageName "Win11_Manufacturing" -SizeGB 60 -WindowsISOPath "D:\Win11.iso"
#>

[CmdletBinding()]
param (
    [Parameter(Mandatory=$true)]
    [string]$ImageName,

    [int]$SizeGB = 60,

    [string]$DestinationDir = ".\disks",

    [string]$WindowsWIMPath = "",

    [string]$DriverDir = ""
)

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "   NKB DISKLESS SYSTEM - GOLDEN IMAGE BUILDER             " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# Phase 2 Safety Policy: Prohibit physical disk or partition modifications
Write-Error "DISABLED_BY_SAFETY_POLICY: Offline golden image staging script is disabled during Phase 2 PoC to prevent accidental disk or partition operations."
exit 1

$sanitizedName = $ImageName.ToLower().Replace(" ", "_")
$vhdxPath = Join-Path (Resolve-Path $DestinationDir) "$sanitizedName.vhdx"

Write-Host "`n[1/4] Creating Dynamic Sparse VHDX ($SizeGB GB)..." -ForegroundColor Yellow
if (Test-Path $vhdxPath) {
    Write-Error "A disk image already exists at $vhdxPath. Please select a different name."
    exit 1
}

# Create sparse VHDX using Diskpart or Hyper-V module
$diskpartScript = @"
create vdisk file="$vhdxPath" maximum=$($SizeGB * 1024) type=expandable
select vdisk file="$vhdxPath"
attach vdisk
convert gpt
create partition efi size=260
format quick fs=fat32 label="System"
assign letter=S
create partition primary
format quick fs=ntfs label="NKB_Windows"
assign letter=W
"@

$diskpartScript | diskpart

Write-Host "  Virtual Disk created and mounted to W:\ and S:\" -ForegroundColor Green

# 2. DISM Apply Image if WIM specified
if ($WindowsWIMPath -and (Test-Path $WindowsWIMPath)) {
    Write-Host "`n[2/4] Applying Windows OS Image to VHDX (DISM)..." -ForegroundColor Yellow
    dism.exe /Apply-Image /ImageFile:"$WindowsWIMPath" /Index:1 /ApplyDir:W:\

    # Setup UEFI Bootloader
    Write-Host "  Configuring UEFI BCD Bootloader on S:\"
    bcdboot.exe W:\Windows /s S: /f UEFI

    # 3. Inject Network Drivers (Crucial for Diskless Booting)
    if ($DriverDir -and (Test-Path $DriverDir)) {
        Write-Host "`n[3/4] Injecting Network Card Drivers (Realtek/Intel)..." -ForegroundColor Yellow
        dism.exe /Image:W:\ /Add-Driver /Driver:"$DriverDir" /Recurse
    }

    # 4. Configure iSCSI Boot Driver in Registry
    Write-Host "`n[4/4] Setting iSCSI Boot Driver Startup to Boot-Critical..." -ForegroundColor Yellow
    reg.exe load HKLM\NKB_SYSTEM W:\Windows\System32\config\SYSTEM
    # Set msiscsi Start = 0 (Boot)
    reg.exe add "HKLM\NKB_SYSTEM\ControlSet001\Services\msiscsi" /v Start /t REG_DWORD /d 0 /f
    reg.exe unload HKLM\NKB_SYSTEM
} else {
    Write-Host "`n[Note] No WIM path specified. Blank initialized VHDX created for manual installation or Super-Client staging." -ForegroundColor Cyan
}

# Detach VHDX
Write-Host "`nDetaching VHDX..."
@"
select vdisk file="$vhdxPath"
detach vdisk
"@ | diskpart

# Register disk with NKB API
Write-Host "`nRegistering Disk in NKB Server..." -ForegroundColor Yellow
try {
    $body = @{
        name = $ImageName
        size_bytes = [int64]$SizeGB * 1024 * 1024 * 1024
        format = "VHDX"
        os_type = "Windows 11 x64"
        description = "Provisioned via prepare-golden-image.ps1"
    } | ConvertTo-Json

    $res = Invoke-RestMethod -Uri "http://localhost:8443/api/v1/disks" -Method Post -ContentType "application/json" -Body $body
    Write-Host "  Successfully registered in NKB Diskless System: $($res.id)" -ForegroundColor Green
} catch {
    Write-Warning "Could not register directly with NKB Server API: $_"
}

Write-Host "`nGolden Master Image is ready: $vhdxPath" -ForegroundColor Green
