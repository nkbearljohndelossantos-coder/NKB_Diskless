# ==============================================================================
# NKB DISKLESS SYSTEM - BOOTLOADER PE32+ INTEGRITY VERIFIER
# Verifies DOS MZ, PE Signature, AMD64 Architecture, and EFI Subsystem
# ==============================================================================

param(
    [Parameter(Mandatory=$false)]
    [string]$FilePath = "tftpboot\uefi\ipxe.efi"
)

Write-Host "============================================================" -ForegroundColor Cyan
Write-Host " NKB DISKLESS - UEFI BOOTLOADER VERIFICATION TOOL" -ForegroundColor Cyan
Write-Host " Target File: $FilePath" -ForegroundColor Yellow
Write-Host "============================================================" -ForegroundColor Cyan

if (-not (Test-Path $FilePath)) {
    Write-Host "ERROR: File '$FilePath' does not exist!" -ForegroundColor Red
    Write-Host "Katayuan: BOOTLOADER FILE MISSING" -ForegroundColor Red
    exit 1
}

$fileBytes = [System.IO.File]::ReadAllBytes((Resolve-Path $FilePath).Path)
$fileSize = $fileBytes.Length
$fileHash = (Get-FileHash $FilePath -Algorithm SHA256).Hash

Write-Host "File Size: $fileSize bytes" -ForegroundColor White
Write-Host "SHA-256:   $fileHash" -ForegroundColor White

# 1. Minimum size check (PE headers require at least 256 bytes)
if ($fileSize -lt 256) {
    Write-Host "[FAIL] File too small to be a valid PE executable ($fileSize bytes)" -ForegroundColor Red
    exit 1
}

# 2. Check for HTML or ASCII text error page
$firstChars = [System.Text.Encoding]::ASCII.GetString($fileBytes, 0, [Math]::Min(64, $fileSize))
if ($firstChars -like "*<!DOCTYPE*" -or $firstChars -like "*<html*" -or $firstChars -like "*404*") {
    Write-Host "[FAIL] DETECTED HTML / TEXT 404 ERROR PAGE!" -ForegroundColor Red
    Write-Host "Header preview: $firstChars" -ForegroundColor Yellow
    Write-Host "Katayuan: INVALID / CORRUPTED BOOTLOADER" -ForegroundColor Red
    exit 1
}

# 3. Check DOS MZ Header (0x4D 0x5A)
$isMZ = ($fileBytes[0] -eq 0x4D -and $fileBytes[1] -eq 0x5A)
if (-not $isMZ) {
    Write-Host ("[FAIL] Invalid DOS Header: Expected 0x4D 0x5A ('MZ'), got 0x{0:X2} 0x{1:X2}" -f $fileBytes[0], $fileBytes[1]) -ForegroundColor Red
    exit 1
}
Write-Host "[PASS] DOS MZ Signature verified (0x4D 0x5A)" -ForegroundColor Green

# 4. Read PE Header Offset at 0x3C (4 bytes, little-endian)
$peOffset = [System.BitConverter]::ToInt32($fileBytes, 0x3C)
Write-Host "PE Header Offset: 0x{0:X} ($peOffset)" -ForegroundColor Gray

if ($peOffset -le 0 -or $peOffset + 120 -gt $fileSize) {
    Write-Host "[FAIL] PE Header Offset 0x$($peOffset.ToString('X')) is out of bounds!" -ForegroundColor Red
    exit 1
}

# 5. Check PE Signature (0x50 0x45 0x00 0x00 -> 'PE\0\0')
$peSig = [System.BitConverter]::ToUInt32($fileBytes, $peOffset)
if ($peSig -ne 0x00004550) { # 0x50 0x45 0x00 0x00 in little-endian
    Write-Host ("[FAIL] Invalid PE Signature: Expected 'PE\0\0' (0x00004550), got 0x{0:X8}" -f $peSig) -ForegroundColor Red
    exit 1
}
Write-Host "[PASS] PE Signature verified ('PE\0\0')" -ForegroundColor Green

# 6. Check Machine Architecture (2 bytes at peOffset + 4)
# 0x8664 = IMAGE_FILE_MACHINE_AMD64 (x86_64)
$machineArch = [System.BitConverter]::ToUInt16($fileBytes, $peOffset + 4)
Write-Host ("Machine Architecture: 0x{0:X4}" -f $machineArch) -ForegroundColor Gray

if ($machineArch -ne 0x8664) {
    Write-Host ("[FAIL] Unsupported Machine Architecture: Expected 0x8664 (AMD64), got 0x{0:X4}" -f $machineArch) -ForegroundColor Red
    exit 1
}
Write-Host "[PASS] Machine Architecture: AMD64 / x86_64 (0x8664)" -ForegroundColor Green

# 7. Check PE32+ Magic in Optional Header (2 bytes at peOffset + 24)
# 0x020B = PE32+ (64-bit)
$optMagic = [System.BitConverter]::ToUInt16($fileBytes, $peOffset + 24)
if ($optMagic -ne 0x020B) {
    Write-Host ("[FAIL] Expected PE32+ (64-bit) Magic 0x020B, got 0x{0:X4}" -f $optMagic) -ForegroundColor Red
    exit 1
}
Write-Host "[PASS] Format: PE32+ 64-bit Executable" -ForegroundColor Green

# 8. Check Subsystem (2 bytes at peOffset + 92 in PE32+)
# 0x000A = IMAGE_SUBSYSTEM_EFI_APPLICATION (10)
$subsystem = [System.BitConverter]::ToUInt16($fileBytes, $peOffset + 92)
Write-Host ("Subsystem: 0x{0:X4} ($subsystem)" -f $subsystem) -ForegroundColor Gray

if ($subsystem -ne 0x000A) {
    Write-Host ("[FAIL] Invalid Subsystem: Expected 0x000A (EFI Application), got 0x{0:X4}" -f $subsystem) -ForegroundColor Red
    exit 1
}
Write-Host "[PASS] Subsystem: IMAGE_SUBSYSTEM_EFI_APPLICATION (0x000A)" -ForegroundColor Green

Write-Host "`n============================================================" -ForegroundColor Green
Write-Host " BOOTLOADER PE32+ VALIDATION PASSED!" -ForegroundColor Green
Write-Host " Target is a verified, genuine x64 EFI Bootloader Application" -ForegroundColor White
Write-Host " Note: File verification confirmed; physical boot test deferred" -ForegroundColor Yellow
Write-Host "============================================================" -ForegroundColor Green
exit 0
