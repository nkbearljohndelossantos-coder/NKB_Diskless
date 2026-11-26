@echo off
title NKB Diskless System - iSCSI Target Server
echo =================================================================
echo        NKB DISKLESS SYSTEM - iSCSI BLOCK STORAGE SERVER
echo =================================================================

:: Check for administrative rights
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo [!] Requesting Administrator privileges...
    powershell -Command "Start-Process '%~f0' -Verb RunAs"
    exit /b
)

cd /d "%~dp0"

echo [1/4] Ensuring Windows Firewall allows TCP port 3260...
netsh advfirewall firewall add rule name="NKB_iSCSI_Target" dir=in action=allow protocol=TCP localport=3260 profile=any >nul 2>&1

echo [2/4] Resolving Master VHDX and Physical Disk Number...
for /f "tokens=*" %%i in ('powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0bin\resolve-disk.ps1"') do set DISK_NUM=%%i

if "%DISK_NUM%"=="" set DISK_NUM=4
echo [✓] Master VHDX is mounted as PhysicalDrive%DISK_NUM%

echo [3/4] Setting PhysicalDrive%DISK_NUM% to OFFLINE state for exclusive iSCSI access...
echo select disk %DISK_NUM% > "%TEMP%\nkb_offline.txt"
echo offline disk >> "%TEMP%\nkb_offline.txt"
diskpart /s "%TEMP%\nkb_offline.txt"
del "%TEMP%\nkb_offline.txt" 2>nul
powershell -NoProfile -ExecutionPolicy Bypass -Command "Set-Disk -Number %DISK_NUM% -IsOffline $true -ErrorAction SilentlyContinue"

echo.
echo [4/4] Starting NKB iSCSI Target Daemon on Port 3260...
echo.
"%~dp0bin\nkb-iscsi.exe" -target "iqn.2026-11.com.nkb:client-2800afa844cc" -disk %DISK_NUM% -port 3260

echo.
echo Daemon stopped or exited.
pause
