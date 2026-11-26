$isoPath = "D:\Win11_25H2_English_x64_v2.iso"
$mount = Mount-DiskImage -ImagePath $isoPath -StorageType ISO -PassThru
$letter = ($mount | Get-Volume).DriveLetter
Write-Host "ISO mounted at drive letter: ${letter}:"

$dest = "C:\NKB-PoC\boot\winpe"
New-Item -ItemType Directory -Path "$dest\boot" -Force | Out-Null
New-Item -ItemType Directory -Path "$dest\sources" -Force | Out-Null

Copy-Item "${letter}:\bootmgr" -Destination "$dest\bootmgr" -Force
Copy-Item "${letter}:\boot\bcd" -Destination "$dest\boot\bcd" -Force
Copy-Item "${letter}:\boot\boot.sdi" -Destination "$dest\boot\boot.sdi" -Force
Copy-Item "${letter}:\sources\boot.wim" -Destination "$dest\sources\boot.wim" -Force

Dismount-DiskImage -ImagePath $isoPath | Out-Null
Write-Host "WinPE staging complete. Files:"
Get-ChildItem -Path $dest -Recurse | Select-Object FullName, Length
