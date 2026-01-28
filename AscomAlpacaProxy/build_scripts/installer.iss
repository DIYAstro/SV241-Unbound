; Inno Setup script for SV241 Ascom Alpaca Proxy
; SEE THE DOCUMENTATION FOR DETAILS ON CREATING INNO SETUP SCRIPT FILES!

#define MyAppName "SV241 Ascom Alpaca Proxy"
#define MyAppPublisher "SV241-Unbound Team"
#define MyAppURL "https://github.com/SV241-Unbound/SV241-Unbound"
#define MyAppExeName "AscomAlpacaProxy.exe"
#define MyAppVersion "##VERSION##"

[Setup]
AppId={{F4F3A3A4-8E44-4B9A-9A8C-5A3D3E2F5B3A}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName=SV241-Unbound
OutputBaseFilename=SV241-AscomAlpacaProxy-Setup-{#MyAppVersion}
OutputDir=.
Compression=lzma
VersionInfoVersion=##FILEVERSION##
VersionInfoCopyright=##COPYRIGHT##
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\{#MyAppExeName}
SetupIconFile=..\icon.ico

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkablealone
Name: "autostart"; Description: "Start &automatically with Windows"; GroupDescription: "Other:"; Flags: checkablealone

[Dirs]
; Creates the Helper directory in the installation folder
Name: "{app}\Helper"

[Files]
Source: "{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\icon.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\Helper\Create-Driver.bat"; DestDir: "{app}\Helper"; Flags: ignoreversion
Source: "..\Helper\Create-AscomDriver.ps1"; DestDir: "{app}\Helper"; Flags: ignoreversion
; NOTE: Don't use "Flags: ignoreversion" on any shared system files

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon
Name: "{group}\Create SV241 Ascom Driver"; Filename: "{app}\Helper\Create-Driver.bat"; WorkingDir: "{app}\Helper"

[Registry]
; Autostart via Registry - controlled by the 'autostart' task
; 1. Add new System-wide (HKLM) entry
Root: HKLM; Subkey: "SOFTWARE\Microsoft\Windows\CurrentVersion\Run"; \
    ValueType: string; ValueName: "{#MyAppName}"; \
    ValueData: """{app}\{#MyAppExeName}"""; \
    Tasks: autostart; Flags: uninsdeletevalue

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[Code]
// Helper function to uninstall previous version
// Returns UninstallString in Result, and InstallLocation in sInstallPath param
function GetUninstallInfo(var sInstallPath: String): String;
var
  sUnInstPathKey: String;
  sUnInstString: String;
  sInstallLoc: String;
begin
  Result := '';
  sInstallPath := '';
  // Hardcoded AppId to ensure we match the registry key exactly (Inno uses single braces in Reg)
  sUnInstPathKey := 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{F4F3A3A4-8E44-4B9A-9A8C-5A3D3E2F5B3A}_is1';

  // Check 32-bit Registry (HKLM32)
  if RegQueryStringValue(HKLM32, sUnInstPathKey, 'UninstallString', sUnInstString) then
  begin
    Result := sUnInstString;
    RegQueryStringValue(HKLM32, sUnInstPathKey, 'InstallLocation', sInstallPath);
  end
  else
  // Check 64-bit Registry (HKLM64)
  if RegQueryStringValue(HKLM64, sUnInstPathKey, 'UninstallString', sUnInstString) then
  begin
    Result := sUnInstString;
    RegQueryStringValue(HKLM64, sUnInstPathKey, 'InstallLocation', sInstallPath);
  end
  else
  // Check HKCU32
  if RegQueryStringValue(HKCU32, sUnInstPathKey, 'UninstallString', sUnInstString) then
  begin
    Result := sUnInstString;
    RegQueryStringValue(HKCU32, sUnInstPathKey, 'InstallLocation', sInstallPath);
  end
  else
  // Check HKCU64
  if RegQueryStringValue(HKCU64, sUnInstPathKey, 'UninstallString', sUnInstString) then
  begin
      Result := sUnInstString;
      RegQueryStringValue(HKCU64, sUnInstPathKey, 'InstallLocation', sInstallPath);
  end;
end;

function InitializeSetup(): Boolean;
var
  sUnInstallString: String;
  sOldInstallPath: String;
  iResultCode: Integer;
begin
  // 0. Force close the application to unlock files
  Exec('taskkill.exe', '/F /IM AscomAlpacaProxy.exe /T', '', SW_HIDE, ewWaitUntilTerminated, iResultCode);

  Result := True;
  sUnInstallString := GetUninstallInfo(sOldInstallPath);
  
  if sUnInstallString <> '' then
  begin
    sUnInstallString := RemoveQuotes(sUnInstallString);
    if Exec(sUnInstallString, '/SILENT /NORESTART /SUPPRESSMSGBOXES', '', SW_HIDE, ewWaitUntilTerminated, iResultCode) then
    begin
        // Successfully uninstalled. Now force-delete the old folder if it still exists.
        // This cleans up leftover config files, logs, etc.
        if (sOldInstallPath <> '') and DirExists(sOldInstallPath) then
        begin
            DelTree(sOldInstallPath, True, True, True);
        end;
    end
    else
    begin
        MsgBox('Failed to uninstall previous version. Please uninstall manually.', mbError, MB_OK);
        Result := False;
    end;
  end;

  // 2. Force cleanup of legacy HKCU Autostart key (silently) to prevent double-start
  // This is done via code to avoid the Inno Setup "PrivilegesRequired" static warning
  if RegKeyExists(HKCU, 'SOFTWARE\Microsoft\Windows\CurrentVersion\Run') then
  begin
    RegDeleteValue(HKCU, 'SOFTWARE\Microsoft\Windows\CurrentVersion\Run', '{#emit SetupSetting("AppName")}');
  end;
end;

// -----------------------------------------------------------------------------
// Uninstaller Logic (When running the Uninstaller manually)
// -----------------------------------------------------------------------------

function InitializeUninstall(): Boolean;
var
  iResultCode: Integer;
begin
  Result := True;
  // Force close the application before uninstalling to ensure files can be removed
  Exec('taskkill.exe', '/F /IM AscomAlpacaProxy.exe /T', '', SW_HIDE, ewWaitUntilTerminated, iResultCode);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
  begin
    // Aggressively clean up the installation directory (including logs/configs) after uninstall
    // Note: The uninstaller extracts itself to temp, so it's safe to delete the target dir now.
    if DirExists(ExpandConstant('{app}')) then
    begin
      DelTree(ExpandConstant('{app}'), True, True, True);
    end;
  end;
end;