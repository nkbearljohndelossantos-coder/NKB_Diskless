$uefiPath = "tftpboot\uefi\ipxe.efi"
$biosPath = "tftpboot\bios\undionly.kpxe"

Write-Host "=== UEFI BOOTLOADER AUDIT ==="
if (Test-Path $uefiPath) {
    $bytes = [System.IO.File]::ReadAllBytes((Resolve-Path $uefiPath).Path)
    Write-Host "File: $uefiPath"
    Write-Host "Size: $($bytes.Length) bytes"
    $hash = (Get-FileHash $uefiPath -Algorithm SHA256).Hash
    Write-Host "SHA-256: $hash"
    $magicHex = "{0:X2} {1:X2}" -f $bytes[0], $bytes[1]
    Write-Host "Bytes 0..1: $magicHex"
    $isMZ = ($bytes[0] -eq 0x4D -and $bytes[1] -eq 0x5A)
    Write-Host "DOS MZ Header Valid: $isMZ"
    $headLen = [Math]::Min(120, $bytes.Length)
    $text = [System.Text.Encoding]::ASCII.GetString($bytes, 0, $headLen)
    Write-Host "Header Content: $text"
} else {
    Write-Host "UEFI file missing!"
}

Write-Host "`n=== BIOS BOOTLOADER AUDIT ==="
if (Test-Path $biosPath) {
    $bytes = [System.IO.File]::ReadAllBytes((Resolve-Path $biosPath).Path)
    Write-Host "File: $biosPath"
    Write-Host "Size: $($bytes.Length) bytes"
    $hash = (Get-FileHash $biosPath -Algorithm SHA256).Hash
    Write-Host "SHA-256: $hash"
    $hex8 = [BitConverter]::ToString($bytes, 0, [Math]::Min(16, $bytes.Length))
    Write-Host "First 16 Bytes: $hex8"
    
    # Search for embedded iPXE strings
    $fullAscii = [System.Text.Encoding]::ASCII.GetString($bytes)
    $hasIPXE = $fullAscii.Contains("iPXE")
    $hasPXENV = $fullAscii.Contains("PXENV+")
    $hasBangPXE = $fullAscii.Contains("!PXE")
    Write-Host "Contains 'iPXE': $hasIPXE"
    Write-Host "Contains 'PXENV+': $hasPXENV"
    Write-Host "Contains '!PXE': $hasBangPXE"
} else {
    Write-Host "BIOS file missing!"
}
