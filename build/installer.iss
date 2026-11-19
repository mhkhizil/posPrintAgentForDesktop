; Inno Setup script for the Lnkits POS Print Agent.
; Build the exe first: .\build\build.ps1 -Version 1.0.0
; Then: iscc build\installer.iss

#define MyAppName "Lnkits POS Print Agent"
#define MyAppExeName "pos-print-agent.exe"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "Lnkits"

[Setup]
AppId={{6C1B9F62-6B7A-4C59-9E2E-2A0C1D4B77A1}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\Lnkits Print Agent
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
OutputDir=..\dist
OutputBaseFilename=lnkits-print-agent-setup
Compression=lzma2
SolidCompression=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayName={#MyAppName}

[Files]
Source: "..\dist\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "--install"; Flags: runhidden waituntilterminated
Filename: "http://127.0.0.1:8735/v1/health"; Flags: shellexec nowait postinstall skipifsilent; Description: "Open the agent health check"

[UninstallRun]
Filename: "{app}\{#MyAppExeName}"; Parameters: "--uninstall"; Flags: runhidden waituntilterminated

[Code]
function InitializeSetup(): Boolean;
begin
  Result := True;
end;
