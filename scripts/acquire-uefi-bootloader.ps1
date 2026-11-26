# ==============================================================================
# NKB DISKLESS SYSTEM - OFFICIAL UEFI BOOTLOADER ACQUISITION & VALIDATION
# Downloads ipxe.efi from official source, verifies PE32+ header, and stages it.
# ==============================================================================

param(
    [string[]]$CandidateUrls = @(
        "http://boot.ipxe.org/ipxe.efi",
        "http://boot.ipxe.org/x86_64-efi/ipxe.efi",
        "https://boot.ipxe.org/ipxe.efi"
    ),
    [string]$StagingFile = "tftpboot\uefi\staging\ipxe.efi",
    [string]$TargetFile = "tftpboot\uefi\ipxe.efi"
)

$ErrorActionPreference = "Stop"

Write-Host "============================================================" -ForegroundColor Cyan
Write-Host " NKB DISKLESS - UEFI BOOTLOADER ACQUISITION TOOL" -ForegroundColor Cyan
Write-Host " Staging: $StagingFile" -ForegroundColor Yellow
Write-Host " Target:  $TargetFile" -ForegroundColor Yellow
Write-Host "============================================================" -ForegroundColor Cyan

# Ensure staging directory exists
$stagingDir = Split-Path $StagingFile -Parent
if (-not (Test-Path $stagingDir)) {
    New-Item -ItemType Directory -Path $stagingDir -Force | Out-Null
}

$downloaded = $false
foreach ($url in $CandidateUrls) {
    Write-Host "[1/3] Trying download from: $url..." -ForegroundColor White
    try {
        $webClient = New-Object System.Net.WebClient
        $webClient.Headers.Add("User-Agent", "Mozilla/5.0")
        $targetStaging = (Resolve-Path $stagingDir).Path + "\ipxe.efi"
        $webClient.DownloadFile($url, $targetStaging)
        
        if (Test-Path $StagingFile) {
            $sz = (Get-Item $StagingFile).Length
            if ($sz -gt 50000) { # genuine ipxe.efi is ~1MB
                Write-Host "[PASS] Successfully downloaded $sz bytes from $url" -ForegroundColor Green
                $downloaded = $true
                break
            } else {
                Write-Host "[WARN] Downloaded file is too small ($sz bytes), likely error page. Retrying..." -ForegroundColor Yellow
            }
        }
    } catch {
        Write-Host "[WARN] Failed from $url : $_" -ForegroundColor Yellow
    }
}

if (-not $downloaded) {
    Write-Host "[FAIL] Could not acquire bootloader from any candidate URL." -ForegroundColor Red
    exit 1
}

$fileSize = (Get-Item $StagingFile).Length
$hash = (Get-FileHash $StagingFile -Algorithm SHA256).Hash
Write-Host "[PASS] Downloaded $fileSize bytes. SHA-256: $hash" -ForegroundColor Green

Write-Host "[2/3] Verifying PE32+ header and AMD64 EFI subsystem..." -ForegroundColor White
& .\scripts\verify-bootloader.ps1 -FilePath $StagingFile
if ($LASTEXITCODE -ne 0) {
    Write-Host "[FAIL] Bootloader failed verification! Quarantining staging file..." -ForegroundColor Red
    Remove-Item $StagingFile -Force
    exit 1
}

Write-Host "[3/3] Verification passed! Staging into $TargetFile..." -ForegroundColor White
Copy-Item -Path $StagingFile -Destination $TargetFile -Force
Write-Host "============================================================" -ForegroundColor Green
Write-Host " SUCCESS: Genuine UEFI bootloader staged at $TargetFile" -ForegroundColor Green
Write-Host " SHA-256: $hash" -ForegroundColor White
Write-Host " Size:    $fileSize bytes" -ForegroundColor White
Write-Host "============================================================" -ForegroundColor Green
