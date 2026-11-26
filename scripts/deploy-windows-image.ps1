<#
.SYNOPSIS
    Automated Windows 11 Master Image Deployment for NKB Diskless System.
.DESCRIPTION
    Mounts a Windows ISO, applies the Windows 11 image to a target VHDX,
    configures the UEFI bootloader, and sets iSCSI drivers to boot-critical.
#>

[CmdletBinding()]
param (
    [Parameter(Mandatory = $false)]
    [string]$ISOPath = "D:\Win11_25H2_English_x64_v2.iso",

    [Parameter(Mandatory = $false)]
    [string]$VHDXPath = "C:\NKB-PoC\disks\virtual\prod_2.vhdx",

    [Parameter(Mandatory = $false)]
    [int]$ImageIndex = 0
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# 1. Check for Elevation; Self-Elevate with -NoExit so window NEVER closes abruptly
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host "Administrative privileges required. Opening Administrator PowerShell window..." -ForegroundColor Yellow
    $argList = "-NoExit -NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`" -ISOPath `"$ISOPath`" -VHDXPath `"$VHDXPath`" -ImageIndex $ImageIndex"
    Start-Process -FilePath "powershell.exe" -ArgumentList $argList -Verb RunAs
    exit 0
}

Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "   NKB DISKLESS SYSTEM — WINDOWS 11 GOLDEN IMAGE BUILDER        " -ForegroundColor Cyan
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "ISO Path   : $ISOPath"
Write-Host "VHDX Path  : $VHDXPath"
Write-Host ""

if (-not (Test-Path $ISOPath)) {
    throw "ISO file not found at: $ISOPath"
}
if (-not (Test-Path $VHDXPath)) {
    throw "Target VHDX file not found at: $VHDXPath"
}

try {
    # 2. Mount ISO
    Write-Host "[1/5] Mounting Windows 11 ISO..." -ForegroundColor Yellow
    $isoMount = Mount-DiskImage -ImagePath $ISOPath -StorageType ISO -PassThru
    Start-Sleep -Seconds 2
    $isoVolume = $isoMount | Get-Volume
    $isoDrive = $isoVolume.DriveLetter
    if (-not $isoDrive) {
        $isoDrive = (Get-Volume | Where-Object { $_.DriveType -eq 'CD-ROM' -and $_.FileSystemLabel -like "*X64*" } | Select-Object -First 1).DriveLetter
    }
    if (-not $isoDrive) {
        throw "Failed to obtain drive letter for mounted ISO."
    }
    Write-Host "  + ISO Mounted at: ${isoDrive}:" -ForegroundColor Green

    $wimFile = Join-Path "${isoDrive}:" "sources\install.wim"
    if (-not (Test-Path $wimFile)) {
        $wimFile = Join-Path "${isoDrive}:" "sources\install.esd"
    }
    if (-not (Test-Path $wimFile)) {
        throw "Neither install.wim nor install.esd found on mounted ISO."
    }
    Write-Host "  + Source WIM Image: $wimFile" -ForegroundColor Green

    # Inspect editions in WIM
    Write-Host "`nScanning editions in image..." -ForegroundColor DarkGray
    $wimOutput = & dism.exe /Get-WimInfo /WimFile:"$wimFile"
    $targetIndex = $ImageIndex
    if ($targetIndex -le 0) {
        $wimString = $wimOutput -join "`n"
        if ($wimString -match "Index\s*:\s*(\d+)\s*\nName\s*:\s*Windows 11 Pro\b") {
            $targetIndex = [int]$Matches[1]
            Write-Host "  + Auto-selected Windows 11 Pro (Index $targetIndex)" -ForegroundColor Green
        } else {
            $targetIndex = 1
            Write-Host "  + Selected Default Image Index: $targetIndex" -ForegroundColor Green
        }
    } else {
        Write-Host "  + Using user-specified Image Index: $targetIndex" -ForegroundColor Green
    }

    # 3. Mount Target VHDX and Initialize/Format Working Partitions (W: and S:)
    Write-Host "`n[2/5] Preparing Target Virtual Disk (VHDX)..." -ForegroundColor Yellow

    # Ensure drive letters W: and S: are not held
    Get-Partition -DriveLetter S -ErrorAction SilentlyContinue | Remove-PartitionAccessPath -AccessPath "S:" -ErrorAction SilentlyContinue | Out-Null
    Get-Partition -DriveLetter W -ErrorAction SilentlyContinue | Remove-PartitionAccessPath -AccessPath "W:" -ErrorAction SilentlyContinue | Out-Null

    # Dismount VHDX first if already attached to get a fresh start
    $existingDiskImage = Get-DiskImage -ImagePath $VHDXPath -ErrorAction SilentlyContinue
    if ($existingDiskImage -and $existingDiskImage.Attached) {
        Write-Host "  + Detaching previously mounted VHDX..." -ForegroundColor DarkGray
        Dismount-DiskImage -ImagePath $VHDXPath -ErrorAction SilentlyContinue | Out-Null
        Start-Sleep -Seconds 2
    }

    # Attach VHDX
    Write-Host "  + Attaching VHDX..." -ForegroundColor DarkGray
    $vhdMount = Mount-DiskImage -ImagePath $VHDXPath -StorageType VHDX -PassThru
    Start-Sleep -Seconds 2
    $disk = $vhdMount | Get-Disk
    if (-not $disk) {
        throw "Failed to locate mounted VHDX disk."
    }
    Write-Host "  + VHDX mounted as Physical Disk Number $($disk.Number)" -ForegroundColor Green

    # Clean disk and initialize GPT partition table
    Write-Host "  + Initializing GPT disk layout and partitions..." -ForegroundColor DarkGray
    Clear-Disk -Number $disk.Number -RemoveData -RemoveOEM -Confirm:$false -ErrorAction SilentlyContinue | Out-Null
    Start-Sleep -Seconds 1
    Initialize-Disk -Number $disk.Number -PartitionStyle GPT -ErrorAction SilentlyContinue | Out-Null
    Start-Sleep -Seconds 1

    # 1. Create EFI System Partition (500 MB)
    Write-Host "  + Creating and formatting EFI System partition (S:)..." -ForegroundColor DarkGray
    $efiPart = New-Partition -DiskNumber $disk.Number -Size 500MB -DriveLetter S
    Start-Sleep -Seconds 1
    Format-Volume -DriveLetter S -FileSystem FAT32 -NewFileSystemLabel "System" -Confirm:$false | Out-Null
    # Set EFI GPT Type
    Set-Partition -DiskNumber $disk.Number -PartitionNumber $efiPart.PartitionNumber -GptType "{c12a7328-f81f-11d2-ba4b-00a0c93ec93b}" -ErrorAction SilentlyContinue | Out-Null

    # 2. Create OS Partition (Remaining Space, NTFS)
    Write-Host "  + Creating and formatting Windows OS partition (W:)..." -ForegroundColor DarkGray
    $osPart = New-Partition -DiskNumber $disk.Number -UseMaximumSize -DriveLetter W
    Start-Sleep -Seconds 1
    Format-Volume -DriveLetter W -FileSystem NTFS -NewFileSystemLabel "Windows" -Confirm:$false | Out-Null

    # Fallback verification & retry if Windows Explorer needed a moment
    $retry = 5
    while ($retry -gt 0 -and (-not (Test-Path "W:\") -or -not (Test-Path "S:\"))) {
        Start-Sleep -Seconds 1
        $retry--
    }

    if (-not (Test-Path "W:\")) {
        throw "OS Drive W:\ is not accessible after attaching and formatting VHDX."
    }
    if (-not (Test-Path "S:\")) {
        throw "EFI System Drive S:\ is not accessible after attaching and formatting VHDX."
    }
    Write-Host "  + Target Partitions Online: OS -> W:\ | EFI Bootloader -> S:\" -ForegroundColor Green

    # 4. Apply Windows Image using DISM
    Write-Host "`n[3/5] Applying Windows 11 Image via DISM (This will take 3-6 minutes)..." -ForegroundColor Yellow
    Write-Host "  Source : $wimFile (Index $targetIndex)" -ForegroundColor DarkGray
    Write-Host "  Target : W:\" -ForegroundColor DarkGray
    
    & dism.exe /Apply-Image /ImageFile:"$wimFile" /Index:$targetIndex /ApplyDir:"W:\"
    if ($LASTEXITCODE -ne 0) {
        throw "DISM Apply-Image failed with exit code: $LASTEXITCODE"
    }
    Write-Host "  + Windows 11 OS applied successfully to W:\" -ForegroundColor Green

    # 5. Configure UEFI BCD Bootloader
    Write-Host "`n[4/5] Initializing UEFI BCD Bootloader on S:\..." -ForegroundColor Yellow
    & bcdboot.exe "W:\Windows" /s S: /f UEFI
    if ($LASTEXITCODE -ne 0) {
        throw "BCDBoot failed with exit code: $LASTEXITCODE"
    }
    Write-Host "  + UEFI BCD Bootloader configured successfully." -ForegroundColor Green

    # 6. Configure iSCSI Boot-Critical Registry Settings
    Write-Host "`n[5/5] Setting iSCSI Boot Driver to Boot-Critical (Start = 0)..." -ForegroundColor Yellow
    & reg.exe load HKLM\NKB_TEMP "W:\Windows\System32\config\SYSTEM" | Out-Null
    & reg.exe add "HKLM\NKB_TEMP\ControlSet001\Services\msiscsi" /v Start /t REG_DWORD /d 0 /f | Out-Null
    & reg.exe unload HKLM\NKB_TEMP | Out-Null
    Write-Host "  + iSCSI Driver set to Boot-Critical in offline registry." -ForegroundColor Green

    Write-Host "`n=================================================================" -ForegroundColor Green
    Write-Host "   WINDOWS 11 GOLDEN MASTER IMAGE SUCCESSFULLY PREPARED!         " -ForegroundColor Green
    Write-Host "=================================================================" -ForegroundColor Green
    Write-Host "Virtual Disk : $VHDXPath"
    Write-Host "Status       : Ready for diskless network booting via iSCSI."
}
catch {
    Write-Host "`n[ERROR OCCURRED]" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    Write-Host "Location: $($_.InvocationInfo.PositionMessage)" -ForegroundColor DarkRed
}
finally {
    Write-Host "`nCleaning up drive attachments..." -ForegroundColor Cyan
    Dismount-DiskImage -ImagePath $VHDXPath -ErrorAction SilentlyContinue | Out-Null
    Dismount-DiskImage -ImagePath $ISOPath -ErrorAction SilentlyContinue | Out-Null
    Write-Host "Drive letters W: and S: released. VHDX cleanly detached." -ForegroundColor Green

    Write-Host "`n-------------------------------------------------------------" -ForegroundColor Cyan
    Write-Host "Tapos na ang proseso. Pindutin ang ENTER para isara ang window na ito..." -ForegroundColor Cyan
    $null = Read-Host
}
