@echo off
set LOG=%~dp0patch_output.txt
echo ================================================= > "%LOG%"
echo NKB PATCH DIAGNOSTIC LOG >> "%LOG%"
echo Time: %date% %time% >> "%LOG%"
echo ================================================= >> "%LOG%"

echo Checking Administrator... >> "%LOG%"
net session >> "%LOG%" 2>&1
if %errorlevel% neq 0 (
    echo NOT RUNNING AS ADMINISTRATOR! >> "%LOG%"
    echo Elevating...
    powershell -Command "Start-Process '%~f0' -Verb RunAs"
    exit /b
)
echo Running as Administrator OK! >> "%LOG%"

echo Stopping iSCSI daemon... >> "%LOG%"
taskkill /F /IM nkb-iscsi.exe >> "%LOG%" 2>&1

echo Checking W: drive... >> "%LOG%"
dir W:\Windows\System32\config\SYSTEM >> "%LOG%" 2>&1

echo Attempting reg load on W:\Windows\System32\config\SYSTEM... >> "%LOG%"
reg unload HKLM\NKB_OFFLINE >> "%LOG%" 2>&1
reg load HKLM\NKB_OFFLINE W:\Windows\System32\config\SYSTEM >> "%LOG%" 2>&1

echo Querying msiscsi Start before edit... >> "%LOG%"
reg query HKLM\NKB_OFFLINE\ControlSet001\Services\msiscsi /v Start >> "%LOG%" 2>&1

echo Setting msiscsi Start = 0... >> "%LOG%"
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\msiscsi" /v Start /t REG_DWORD /d 0 /f >> "%LOG%" 2>&1

echo Setting iscsiprt Start = 0... >> "%LOG%"
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\iScsiPrt" /v Start /t REG_DWORD /d 0 /f >> "%LOG%" 2>&1

echo Setting ndis Start = 0... >> "%LOG%"
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\ndis" /v Start /t REG_DWORD /d 0 /f >> "%LOG%" 2>&1

echo Setting tcpip Start = 0... >> "%LOG%"
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\tcpip" /v Start /t REG_DWORD /d 0 /f >> "%LOG%" 2>&1

echo Setting rt640x64 Start = 0... >> "%LOG%"
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt640x64" /v Start /t REG_DWORD /d 0 /f >> "%LOG%" 2>&1

echo Setting rtcx21x64 Start = 0... >> "%LOG%"
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rtcx21x64" /v Start /t REG_DWORD /d 0 /f >> "%LOG%" 2>&1

echo Setting rt68cx21 Start = 0... >> "%LOG%"
reg add "HKLM\NKB_OFFLINE\ControlSet001\Services\rt68cx21" /v Start /t REG_DWORD /d 0 /f >> "%LOG%" 2>&1

echo Querying msiscsi Start after edit... >> "%LOG%"
reg query HKLM\NKB_OFFLINE\ControlSet001\Services\msiscsi /v Start >> "%LOG%" 2>&1

echo Unloading hive... >> "%LOG%"
reg unload HKLM\NKB_OFFLINE >> "%LOG%" 2>&1

echo Dismounting prod_2.vhdx... >> "%LOG%"
powershell -Command "Dismount-DiskImage -ImagePath 'C:\NKB-PoC\disks\virtual\prod_2.vhdx'" >> "%LOG%" 2>&1

echo Converting to prod_2.vhd... >> "%LOG%"
del "C:\NKB-PoC\disks\virtual\prod_2.vhd" >> "%LOG%" 2>&1
"%~dp0tools\qemu10\qemu-img.exe" check -r all "C:\NKB-PoC\disks\virtual\prod_2.vhdx" >> "%LOG%" 2>&1
"%~dp0tools\qemu10\qemu-img.exe" convert -f vhdx -O vpc -o subformat=dynamic "C:\NKB-PoC\disks\virtual\prod_2.vhdx" "C:\NKB-PoC\disks\virtual\prod_2.vhd" >> "%LOG%" 2>&1

echo Starting iSCSI daemon... >> "%LOG%"
start "" "%~dp0bin\nkb-iscsi.exe" -target "iqn.2026-11.com.nkb:client-2800afa844cc" -vhd "C:\NKB-PoC\disks\virtual\prod_2.vhd" -port 3260

echo DIAGNOSTIC COMPLETE! Check %LOG%
pause
