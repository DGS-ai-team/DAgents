#define MyAppName "DAgents Local Assistant"
#ifndef MyAppVersion
#define MyAppVersion "0.0.0"
#endif
#ifndef MyAppArch
#define MyAppArch "x64"
#endif
#ifndef MyOutputBaseFilename
#define MyOutputBaseFilename "dagents-local-assistant-windows-amd64-installer"
#endif

[Setup]
AppId={{A3B8C2D1-9E4F-4A7B-8C6D-1E2F3A4B5C6D}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=DAgents
DefaultDirName={autopf}\DAgents
DefaultGroupName=DAgents
DisableProgramGroupPage=yes
OutputDir=..\..\dist-installer
OutputBaseFilename={#MyOutputBaseFilename}
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed={#MyAppArch}
ArchitecturesInstallIn64BitMode=x64
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
UninstallDisplayIcon={app}\bin\dagents-node.exe
ChangesEnvironment=yes

[Files]
; 二进制与安装脚本：升级时始终覆盖
Source: "..\..\bundle\bin\*"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "dagents.cmd"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\bundle\scripts\*"; DestDir: "{app}\scripts"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\..\bundle\config.example.yaml"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\.env.example"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\README.txt"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\VERSION"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
; .runtime：仅补缺失路径，不覆盖已有 policy / skills / prompt_context 等
Source: "..\..\bundle\.runtime\*"; DestDir: "{app}\.runtime"; Flags: recursesubdirs onlyifdoesntexist createallsubdirs

[Icons]
Name: "{group}\DAgents Shell"; Filename: "{cmd}"; Parameters: "/K cd /d ""{app}"" && dagents help"; WorkingDir: "{app}"
Name: "{group}\Start Agent Node (background)"; Filename: "{app}\dagents.cmd"; Parameters: "node"; WorkingDir: "{app}"
Name: "{group}\Start Agent Node (foreground)"; Filename: "{app}\dagents.cmd"; Parameters: "node --foreground"; WorkingDir: "{app}"
Name: "{group}\Chat (Textual TUI)"; Filename: "{app}\dagents.cmd"; Parameters: "chat --withnode"; WorkingDir: "{app}"
Name: "{group}\TUI (Go full-screen)"; Filename: "{app}\dagents.cmd"; Parameters: "tui --withnode"; WorkingDir: "{app}"
Name: "{group}\REPL (Go line mode)"; Filename: "{app}\dagents.cmd"; Parameters: "tui --withnode --plain"; WorkingDir: "{app}"

[Registry]

[Run]
Filename: "{app}\dagents.cmd"; Parameters: "doctor"; Description: "Verify installed files (dagents doctor)"; Flags: postinstall skipifsilent runascurrentuser

[Code]
function PathContains(const Path, Dir: string): Boolean;
begin
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') <> 0;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
  AppDir: string;
begin
  Result := '';
  AppDir := ExpandConstant('{app}');
  if FileExists(AppDir + '\dagents.cmd') then
    Exec(AppDir + '\dagents.cmd', 'node shutdown', AppDir, SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Path, AppDir, ScriptsDir: string;
begin
  if CurStep <> ssPostInstall then
    Exit;
  AppDir := ExpandConstant('{app}');
  ScriptsDir := AppDir + '\.runtime\scripts';
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
    Path := '';
  if not PathContains(Path, AppDir) then
    Path := Path + ';' + AppDir;
  if not PathContains(Path, ScriptsDir) then
    Path := Path + ';' + ScriptsDir;
  RegWriteExpandStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path);
end;
