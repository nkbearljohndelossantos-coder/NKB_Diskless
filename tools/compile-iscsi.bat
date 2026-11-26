@echo off
set CSC=C:\Windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe
set REF=%~dp0..\tools\iscsi\net472\ISCSIConsole.exe
set SRC=%~dp0..\tools\iscsi_daemon\Program.cs
set OUT=%~dp0..\bin\nkb-iscsi.exe

echo Compiling %SRC% with %CSC%...
"%CSC%" /nologo /target:exe /out:"%OUT%" /r:"%REF%" "%SRC%"
if %errorlevel% equ 0 (
    echo SUCCESS! Created %OUT%
    dir "%OUT%"
) else (
    echo COMPILATION FAILED!
)
