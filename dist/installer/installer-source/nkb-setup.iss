; NKB Diskless System — Inno Setup Script
; Architecture: Windows 11 Pro x64
; Version: 1.0.0

#define MyAppName "NKB Diskless System"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "NKB Diskless Infrastructure"
#define MyAppURL "https://github.com/nkb-diskless"
#define MyAppExeName "nkb-server.exe"

[Setup]
AppId={{8F1E902B-5C87-4E2E-A341-94BC6C8D89A1}}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
AllowNoIcons=yes
OutputDir=..\
OutputBaseFilename=NKB-Diskless-System-Setup-x64
Compression=lzma2/max
SolidCompression=yes
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog commandline
DisableDirPage=no
DisableProgramGroupPage=no
SetupIconFile=resources\app.ico
UninstallIconFile=resources\app.ico
WizardImageFile=resources\wizard-large.bmp
WizardSmallImageFile=resources\wizard-small.bmp
UninstallDisplayIcon={app}\resources\app.ico
CloseApplications=force
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Types]
Name: "full"; Description: "Full Installation (Recommended)"
Name: "custom"; Description: "Custom Installation"; Flags: iscustom

[Components]
Name: "core"; Description: "NKB Diskless Core Services & Binaries"; Types: full custom; Flags: fixed
Name: "web"; Description: "Web Management Dashboard"; Types: full custom
Name: "boot"; Description: "PXE & iPXE Network Bootloaders"; Types: full custom
Name: "tools"; Description: "Hardware Discovery & Staging Scripts"; Types: full custom

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce
Name: "firewall"; Description: "Configure Windows Defender Firewall rules for NKB PXE & Management ports"; GroupDescription: "Network Security:"
Name: "service"; Description: "Register NKB Server as a Windows Service (Manual startup)"; GroupDescription: "System Integration:"

[Files]
; Core Binaries
Source: "..\..\staging\bin\*"; DestDir: "{app}\bin"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: core
; Web Dashboard UI
Source: "..\..\staging\web\*"; DestDir: "{app}\web"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: web
; Bootloaders (BIOS & UEFI)
Source: "..\..\staging\tftpboot\*"; DestDir: "{app}\tftpboot"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: boot
; Database Migrations
Source: "..\..\staging\migrations\*"; DestDir: "{app}\migrations"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: core
; Configuration Templates (do not overwrite existing live config)
Source: "..\..\staging\config\*.template.json"; DestDir: "{app}\config"; Flags: ignoreversion; Components: core
; Staging & Rollback Scripts
Source: "..\..\staging\config\scripts\*"; DestDir: "{app}\config\scripts"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: tools
Source: "..\..\staging\config\snapshots\*"; DestDir: "{app}\config\snapshots"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: tools
; Setup Helper Scripts
Source: "first-run-setup.ps1"; DestDir: "{app}\config\scripts"; Flags: ignoreversion; Components: tools
Source: "install-service.ps1"; DestDir: "{app}\config\scripts"; Flags: ignoreversion; Components: tools
Source: "uninstall-service.ps1"; DestDir: "{app}\config\scripts"; Flags: ignoreversion; Components: tools
Source: "configure-firewall.ps1"; DestDir: "{app}\config\scripts"; Flags: ignoreversion; Components: tools
; Documentation
Source: "..\..\staging\docs\*"; DestDir: "{app}\docs"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: core
; Branding Assets (Icons, Logos, Banners)
Source: "resources\*"; DestDir: "{app}\resources"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: core

[Icons]
Name: "{group}\NKB Desktop Manager"; Filename: "{app}\bin\nkb-desktop-manager.exe"; IconFilename: "{app}\resources\app.ico"
Name: "{group}\NKB Web Management Dashboard"; Filename: "{cmd}"; Parameters: "/c start http://localhost:8443"; IconFilename: "{app}\resources\app.ico"
Name: "{group}\NKB First-Run Setup Wizard"; Filename: "powershell.exe"; Parameters: "-ExecutionPolicy Bypass -NoExit -File ""{app}\config\scripts\first-run-setup.ps1"" -InstallDir ""{app}"""; IconFilename: "{app}\resources\app.ico"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"; IconFilename: "{app}\resources\app.ico"
Name: "{autodesktop}\NKB Desktop Manager"; Filename: "{app}\bin\nkb-desktop-manager.exe"; Tasks: desktopicon; IconFilename: "{app}\resources\app.ico"

[Run]
; Optional Firewall Configuration
Filename: "powershell.exe"; Parameters: "-ExecutionPolicy Bypass -File ""{app}\config\scripts\configure-firewall.ps1"" -Action Enable"; Tasks: firewall; Flags: runhidden
; Optional Windows Service Registration
Filename: "powershell.exe"; Parameters: "-ExecutionPolicy Bypass -File ""{app}\config\scripts\install-service.ps1"" -InstallDir ""{app}"""; Tasks: service; Flags: runhidden
; Post-Install First-Run Hardware Onboarding Launch
Filename: "powershell.exe"; Parameters: "-ExecutionPolicy Bypass -NoExit -File ""{app}\config\scripts\first-run-setup.ps1"" -InstallDir ""{app}"""; Description: "Launch First-Run Hardware Setup Wizard (Discovers network adapters & configures profile)"; Flags: postinstall nowait skipifsilent

[UninstallRun]
; Cleanly remove service if installed
Filename: "powershell.exe"; Parameters: "-ExecutionPolicy Bypass -File ""{app}\config\scripts\uninstall-service.ps1"""; Flags: runhidden; RunOnceId: "RemoveNKBService"
; Cleanly remove firewall rules if installed
Filename: "powershell.exe"; Parameters: "-ExecutionPolicy Bypass -File ""{app}\config\scripts\configure-firewall.ps1"" -Action Remove"; Flags: runhidden; RunOnceId: "RemoveNKBFirewallRules"

[Code]
// Pascal Scripting for Upgrade Protection and Clean Uninstallation

// Backup existing configuration during upgrade
procedure CurStepChanged(CurStep: TSetupStep);
var
  ConfigPath, BackupDir, TimeStamp: string;
begin
  if CurStep = ssInstall then
  begin
    ConfigPath := ExpandConstant('{app}\config');
    if DirExists(ConfigPath) then
    begin
      // If live configs exist, back them up
      if FileExists(ConfigPath + '\network_profile.json') or FileExists(ConfigPath + '\nkb_config.json') then
      begin
        TimeStamp := GetDateTimeString('yyyymmdd_hhnnss', '-', '-');
        BackupDir := ConfigPath + '\backup\' + TimeStamp;
        CreateDir(ConfigPath + '\backup');
        CreateDir(BackupDir);
        if FileExists(ConfigPath + '\network_profile.json') then
          CopyFile(ConfigPath + '\network_profile.json', BackupDir + '\network_profile.json', False);
        if FileExists(ConfigPath + '\nkb_config.json') then
          CopyFile(ConfigPath + '\nkb_config.json', BackupDir + '\nkb_config.json', False);
        Log('Existing configuration backed up to ' + BackupDir);
      end;
    end;
  end;
end;

// Preserve user data and storage directories on uninstall
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
  begin
    Log('Uninstallation complete. User data in storage directories preserved.');
  end;
end;
