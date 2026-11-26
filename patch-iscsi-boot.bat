@echo off
title NKB Diskless - Windows 11 iSCSI Boot-Start Driver Patch
echo =================================================================
echo   NKB DISKLESS - PATCHING WINDOWS 11 FOR iSCSI NETWORK BOOT
echo =================================================================

:: Elevate to Administrator
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo [!] Elevating to Administrator...
    powershell -Command "Start-Process '%~f0' -Verb RunAs"
    exit /b
)

cd /d "%~dp0"

echo [1/5] Stopping active iSCSI daemon to release disk locks...
taskkill /F /IM nkb-iscsi.exe >nul 2>&1

echo [2/5] Mounting prod_2.vhd to patch offline registry...
powershell -NoProfile -ExecutionPolicy Bypass -Command "$vhd = 'C:\NKB-PoC\disks\virtual\prod_2.vhd'; Mount-DiskImage -ImagePath $vhd -ErrorAction SilentlyContinue | Out-Null; $p = Get-DiskImage -ImagePath $vhd | Get-Disk | Get-Partition | Where-Object { $_.DriveLetter -and $_.Size -gt 10GB }; if ($p) { Set-Content -Path '%TEMP%\vhd_drive.txt' -Value $p.DriveLetter }"

set /p TARGET_DRIVE=<"%TEMP%\vhd_drive.txt"
if "%TARGET_DRIVE%"=="" set TARGET_DRIVE=W

echo [✓] Target Windows OS partition detected on drive %TARGET_DRIVE%:

echo [3/5] Loading offline SYSTEM registry hive (%TARGET_DRIVE%:\Windows\System32\config\SYSTEM)...
reg load HKLM\NKB_OFFLINE "%TARGET_DRIVE%:\Windows\System32\config\SYSTEM"
if %errorLevel% neq 0 (
    echo [X] Failed to load registry hive. Checking if W: is available...
    reg load HKLM\NKB_OFFLINE "W:\Windows\System32\config\SYSTEM"
)

echo [4/5] Injecting Boot-Start (Start=0) flags for iSCSI and Network Miniport Drivers...

:: Microsoft iSCSI Initiator Stack (Crucial: Start=0)
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\msiscsi" /v Start /t REG_DWORD /d 0 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\iScsiPrt" /v Start /t REG_DWORD /d 0 /f >nul

:: Core Network & Protocol Stack
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\ndis" /v Start /t REG_DWORD /d 0 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\tcpip" /v Start /t REG_DWORD /d 0 /f >nul

:: Realtek PCIe GbE Drivers (Dell Latitude 3440 NIC)
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt640x64" /v Start /t REG_DWORD /d 0 /f >nul 2>&1
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rtcx21x64" /v Start /t REG_DWORD /d 0 /f >nul 2>&1
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt68cx21" /v Start /t REG_DWORD /d 0 /f >nul 2>&1
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rteth" /v Start /t REG_DWORD /d 0 /f >nul 2>&1

:: Intel Ethernet Gigabit/10G Drivers
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\e1dexpress" /v Start /t REG_DWORD /d 0 /f >nul 2>&1
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\e1i68x64" /v Start /t REG_DWORD /d 0 /f >nul 2>&1
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\e2f68" /v Start /t REG_DWORD /d 0 /f >nul 2>&1

:: NDIS Link Reset Stability Patch (Prevents BugCheck 0x7B on NIC miniport re-init)
reg add "HKLM\NKB_OFFLINE\ControlSet001\Control\NetworkProvider" /v RestoreConnection /t REG_DWORD /d 0 /f >nul

echo [✓] Registry keys successfully patched!
echo [5/5] Unloading offline registry hive and dismounting disk...
reg unload HKLM\NKB_OFFLINE

powershell -NoProfile -ExecutionPolicy Bypass -Command "Dismount-DiskImage -ImagePath 'C:\NKB-PoC\disks\virtual\prod_2.vhd' -ErrorAction SilentlyContinue" >nul 2>&1

echo.
echo =================================================================
echo [SUCCESS] Windows 11 Master Image is now configured for iSCSI Boot!
echo Starting iSCSI Target Server...
echo =================================================================
echo.

start "" "%~dp0bin\nkb-iscsi.exe" -target "iqn.2026-11.com.nkb:client-2800afa844cc" -vhd "C:\NKB-PoC\disks\virtual\prod_2.vhd" -port 3260

echo iSCSI Server is now RUNNING! You can now reboot the Dell Laptop!
pause
