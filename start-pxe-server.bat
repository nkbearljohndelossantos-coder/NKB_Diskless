@echo off
title NKB Diskless System - PXE Boot Daemon
echo =================================================================
echo   NKB DISKLESS SYSTEM - PXE ^& TFTP BOOT DAEMON
echo =================================================================

:: Check for administrative rights
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo Elevating to Administrator...
    powershell -Command "Start-Process '%~f0' -Verb RunAs"
    exit /b
)

cd /d "%~dp0"

echo [1/2] Optimizing Network Adapters and Firewall for PXE Boot...
powershell -NoProfile -Command "Set-NetConnectionProfile -InterfaceAlias 'Ethernet' -NetworkCategory Private -ErrorAction SilentlyContinue; Set-NetIPInterface -InterfaceAlias 'Ethernet' -InterfaceMetric 5 -ErrorAction SilentlyContinue; Get-NetIPInterface | Where-Object { $_.InterfaceAlias -like '*WSL*' -or $_.InterfaceAlias -like '*vEthernet*' } | Set-NetIPInterface -InterfaceMetric 500 -ErrorAction SilentlyContinue" >nul 2>&1
netsh advfirewall firewall add rule name="NKB_PXE_TFTP" dir=in action=allow protocol=UDP localport=67,68,69,4011 profile=any >nul 2>&1
netsh advfirewall firewall add rule name="NKB_ISCSI_HTTP" dir=in action=allow protocol=TCP localport=3260,8443,8080 profile=any >nul 2>&1
route change 255.255.255.255 mask 255.255.255.255 192.168.254.100 metric 1 >nul 2>&1

echo [2/2] Starting NKB PXE and TFTP Boot Listeners...
echo.
"%~dp0bin\nkb-pxe.exe" -ip 192.168.254.100 -boot-dir "C:\NKB-PoC\boot" -http-port 8443

pause
