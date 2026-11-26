@echo off
title NKB Diskless System - DISM PnP and 0x7B Boot Fix
color 0A

:: Check elevation
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo Elevating to Administrator...
    powershell -NoProfile -Command "Start-Process cmd.exe -ArgumentList '/k \"\"%~f0\"\"' -Verb RunAs"
    exit /b
)

cd /d "%~dp0"

echo =================================================================
echo   NKB DISKLESS SYSTEM - DISM DRIVER INJECTION AND 0x7B BOOT FIX
echo =================================================================
echo.

echo [1/7] Stopping iSCSI daemon...
taskkill /F /IM nkb-iscsi.exe >nul 2>&1

echo [2/7] Checking Drive W: and S: mounts...
if not exist "W:\Windows\System32" (
    echo [!] Drive W: not found. Mounting prod_2.vhdx...
    powershell -NoProfile -ExecutionPolicy Bypass -Command "Mount-DiskImage -ImagePath 'C:\NKB-PoC\disks\virtual\prod_2.vhdx' | Out-Null"
    timeout /t 3 >nul
)

echo [3/7] Registering Realtek RTL8111/8168 NIC Driver with DISM...
dism.exe /Image:W:\ /Add-Driver /Driver:"W:\Windows\System32\DriverStore\FileRepository\rt640x64.inf_amd64_cbf4e23981c3c8a3\rt640x64.inf" /ForceUnsigned

:: Ensure driver is also in drivers directory
copy /y "W:\Windows\System32\DriverStore\FileRepository\rt640x64.inf_amd64_cbf4e23981c3c8a3\rt640x64.sys" "W:\Windows\System32\drivers\rt640x64.sys" >nul
if exist "W:\Windows\System32\drivers\Rtnic64.sys" del /f /q "W:\Windows\System32\drivers\Rtnic64.sys" >nul

echo [4/7] Patching Boot-Start Services and BootDriverFlags in Registry...
reg load HKLM\NKB_OFFLINE W:\Windows\System32\config\SYSTEM
if %errorLevel% neq 0 (
    echo [!] Failed to load SYSTEM hive from W:\Windows\System32\config\SYSTEM
    echo [!] Please close any programs accessing Drive W: and try again.
    pause
    exit /b 1
)

:: Enable iSCSI BootDriverFlags (Tells Windows kernel this is a network boot)
reg add "HKLM\NKB_OFFLINE\ControlSet001\Control" /v BootDriverFlags /t REG_DWORD /d 1 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Control\PnP" /v BootDriverFlags /t REG_DWORD /d 1 /f >nul

:: Configure iSCSI Initiator as Boot-Critical
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\msiscsi" /v Start /t REG_DWORD /d 0 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\msiscsi" /v BootFlags /t REG_DWORD /d 1 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\msiscsi" /v Type /t REG_DWORD /d 1 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\msiscsi" /v ErrorControl /t REG_DWORD /d 1 /f >nul

:: Configure Realtek rt640x64 as Boot-Critical NDIS driver
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt640x64" /v Start /t REG_DWORD /d 0 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt640x64" /v BootFlags /t REG_DWORD /d 1 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt640x64" /v Type /t REG_DWORD /d 1 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt640x64" /v ErrorControl /t REG_DWORD /d 1 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt640x64" /v ImagePath /t REG_EXPAND_SZ /d "system32\drivers\rt640x64.sys" /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt640x64" /v Group /t REG_SZ /d "NDIS" /f >nul

:: Configure Core Network Stack as Boot-Critical
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\ndis" /v Start /t REG_DWORD /d 0 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\ndis" /v BootFlags /t REG_DWORD /d 1 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\tcpip" /v Start /t REG_DWORD /d 0 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\tcpip" /v BootFlags /t REG_DWORD /d 1 /f >nul
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\tcpipreg" /v Start /t REG_DWORD /d 0 /f >nul

:: Delete any conflicting legacy services
reg delete "HKLM\NKB_OFFLINE\ControlSet001\Services\Rtnic64" /f >nul 2>&1
reg delete "HKLM\NKB_OFFLINE\ControlSet001\Services\iScsiPrt" /f >nul 2>&1

:: Disable Network Provider reconnection delays
reg add "HKLM\NKB_OFFLINE\ControlSet001\Control\NetworkProvider" /v RestoreConnection /t REG_DWORD /d 0 /f >nul

echo [5/7] Flushing registry changes and unloading hive...
reg unload HKLM\NKB_OFFLINE
timeout /t 2 >nul

:: Configure BCD for verbose driver logging if S: exists
if exist "S:\EFI\Microsoft\Boot\BCD" (
    echo [*] Enabling verbose driver boot logging in BCD...
    bcdedit /store S:\EFI\Microsoft\Boot\BCD /set {default} bootlog Yes >nul 2>&1
    bcdedit /store S:\EFI\Microsoft\Boot\BCD /set {default} sos Yes >nul 2>&1
    bcdedit /store S:\EFI\Microsoft\Boot\BCD /set {default} bootstatuspolicy IgnoreAllFailures >nul 2>&1
)

echo [6/7] Exporting updated bootable VHD (qemu-img)...
echo Copying prod_2.vhdx to temporary conversion image...
copy /y "C:\NKB-PoC\disks\virtual\prod_2.vhdx" "C:\NKB-PoC\disks\virtual\prod_temp.vhdx" >nul

echo Converting to dynamic VHD...
if exist "C:\NKB-PoC\disks\virtual\prod_2.vhd" del "C:\NKB-PoC\disks\virtual\prod_2.vhd"
"%~dp0tools\qemu10\qemu-img.exe" convert -f vhdx -O vpc -o subformat=dynamic "C:\NKB-PoC\disks\virtual\prod_temp.vhdx" "C:\NKB-PoC\disks\virtual\prod_2.vhd"

del "C:\NKB-PoC\disks\virtual\prod_temp.vhdx" >nul 2>&1

echo [7/7] Starting NKB iSCSI Target Daemon...
start "" "%~dp0bin\nkb-iscsi.exe" -target "iqn.2026-11.com.nkb:client-2800afa844cc" -vhd "C:\NKB-PoC\disks\virtual\prod_2.vhd" -port 3260

echo.
echo =================================================================
echo [OK] SUCCESS! DISM INJECTED AND ISCSI DAEMON IS ACTIVE!
echo =================================================================
echo.
echo >>> NOW RESTART THE DELL LATITUDE 3440 LAPTOP! <<<
echo.
pause
