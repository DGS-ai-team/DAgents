#define MyAppName "DAgents Backend"
#ifndef MyAppVersion
#define MyAppVersion "0.0.0"
#endif
#ifndef MyAppArch
#define MyAppArch "x64"
#endif
#ifndef MyOutputBaseFilename
#define MyOutputBaseFilename "dagents-backend-windows-x64-installer"
#endif

[Setup]
AppId={{7C7D8A59-4D5B-44D0-9E52-DAE7D0D353A1}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=DAgents
DefaultDirName={autopf}\DAgents\Backend
DefaultGroupName=DAgents
DisableProgramGroupPage=yes
OutputDir=dist-installer
OutputBaseFilename={#MyOutputBaseFilename}
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed={#MyAppArch}
ArchitecturesInstallIn64BitMode=x64
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
UninstallDisplayIcon={app}\dagents-cli.exe
ChangesEnvironment=yes

[Files]
; CI 与本地均在「仓库根」生成 bundle\；脚本在 packaging\windows\，故用 ..\..\ 回到根目录再取 bundle。
Source: "..\..\bundle\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
; 启动包装脚本与本 .iss 同目录，勿写 packaging\windows\ 前缀（否则会变成 packaging\windows\packaging\windows\...）。
Source: "dagents.cmd"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\DAgents Backend Shell"; Filename: "{cmd}"; Parameters: "/K cd /d ""{app}"" && dagents help"; WorkingDir: "{app}"
Name: "{group}\DAgents Chat"; Filename: "{app}\dagents.cmd"; Parameters: "chat"; WorkingDir: "{app}"
Name: "{group}\Start DAgents Backend"; Filename: "{app}\dagents.cmd"; Parameters: "serve"; WorkingDir: "{app}"

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPath(ExpandConstant('{app}'))

[Run]
Filename: "{app}\dagents.cmd"; Parameters: "doctor"; Description: "Run dagents doctor"; Flags: postinstall skipifsilent runascurrentuser

[Code]
function NeedsAddPath(Dir: string): Boolean;
var
  Path: string;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
    Path := '';
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') = 0;
end;
