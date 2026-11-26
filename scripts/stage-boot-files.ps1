$ErrorActionPreference = 'Stop'
$destDir = 'C:\NKB-PoC\boot'
if (-not (Test-Path $destDir)) {
    New-Item -ItemType Directory -Path $destDir -Force | Out-Null
}

$uefiDir = Join-Path $destDir 'uefi'
$biosDir = Join-Path $destDir 'bios'
New-Item -ItemType Directory -Path $uefiDir -Force | Out-Null
New-Item -ItemType Directory -Path $biosDir -Force | Out-Null

$srcEfi = Join-Path $PSScriptRoot '..\tftpboot\uefi\ipxe.efi'
$srcBios = Join-Path $PSScriptRoot '..\tftpboot\bios\undionly.kpxe'

Copy-Item -Path $srcEfi -Destination (Join-Path $destDir 'ipxe.efi') -Force
Copy-Item -Path $srcEfi -Destination (Join-Path $destDir 'bootx64.efi') -Force
Copy-Item -Path $srcEfi -Destination (Join-Path $destDir 'snponsonly.efi') -Force
Copy-Item -Path $srcEfi -Destination (Join-Path $uefiDir 'ipxe.efi') -Force
Copy-Item -Path $srcEfi -Destination (Join-Path $uefiDir 'bootx64.efi') -Force
Copy-Item -Path $srcEfi -Destination (Join-Path $uefiDir 'snponsonly.efi') -Force

Copy-Item -Path $srcBios -Destination (Join-Path $destDir 'undionly.kpxe') -Force
Copy-Item -Path $srcBios -Destination (Join-Path $biosDir 'undionly.kpxe') -Force

Write-Host "Staged boot files successfully in ${destDir}:" -ForegroundColor Green
Get-ChildItem -Path $destDir -Recurse | Select-Object FullName
