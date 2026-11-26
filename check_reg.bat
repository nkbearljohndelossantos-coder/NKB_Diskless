@echo off
reg load HKLM\NKB_CHECK W:\Windows\System32\config\SYSTEM >nul 2>&1
echo --- BootDriverFlags --- > "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt"
reg query "HKLM\NKB_CHECK\ControlSet001\Control" /v BootDriverFlags >> "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt" 2>&1
echo --- msiscsi --- >> "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt"
reg query "HKLM\NKB_CHECK\ControlSet001\Services\msiscsi" /v Start >> "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt" 2>&1
echo --- rt640x64 --- >> "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt"
reg query "HKLM\NKB_CHECK\ControlSet001\Services\rt640x64" /v Start >> "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt" 2>&1
reg query "HKLM\NKB_CHECK\ControlSet001\Services\rt640x64" /v Group >> "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt" 2>&1
echo --- tcpip --- >> "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt"
reg query "HKLM\NKB_CHECK\ControlSet001\Services\tcpip" /v Start >> "c:\Users\earlj\Desktop\NKB Diskless System Management\reg_check.txt" 2>&1
reg unload HKLM\NKB_CHECK >nul 2>&1
