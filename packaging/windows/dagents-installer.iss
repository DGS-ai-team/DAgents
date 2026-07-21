#define MyAppName "DAgents 本地助手"
#ifndef MyAppVersion
#define MyAppVersion "0.0.0"
#endif
#ifndef MyAppArch
#define MyAppArch "x64"
#endif
#ifndef MyOutputBaseFilename
#define MyOutputBaseFilename "dagents-local-assistant-windows-amd64-installer"
#endif
#define ShellAutostartRunName "DAgents Shell"
#define ShellAutostartRegKey "Software\Microsoft\Windows\CurrentVersion\Run"

[Setup]
AppId={{A3B8C2D1-9E4F-4A7B-8C6D-1E2F3A4B5C6D}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=DAgents
AppPublisherURL=https://github.com/DGS-ai-team/DAgents
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
WizardStyle=modern
WizardSizePercent=110
ShowLanguageDialog=no
SetupIconFile=..\..\desktop\tray\assets\icon.ico
WizardImageFile=assets\wizard-sidebar.bmp
WizardSmallImageFile=assets\wizard-small.bmp
WizardImageStretch=no
DisableWelcomePage=no
DisableFinishedPage=no

[Languages]
Name: "chinesesimp"; MessagesFile: "Languages\ChineseSimplified.isl"

[CustomMessages]
chinesesimp.WelcomeLabel2=安装完成后请打开 Web UI「设置 › 连接」配置 LLM、Manage 与功能开关；API Key 请写入系统环境变量（如 OPENAI_API_KEY）。
chinesesimp.WizardSelectDir=选择安装位置
chinesesimp.WizardSelectDirLabel3=DAgents 将安装到以下文件夹。
chinesesimp.WizardSelectTasks=附加任务
chinesesimp.WizardSelectTasksLabel2=选择安装完成后要执行的附加任务。
chinesesimp.WizardReady=准备安装
chinesesimp.WizardReadyLabel1=安装程序已准备好将 DAgents 安装到您的计算机。
chinesesimp.WizardReadyLabel2a=点击「安装」开始，或点击「上一步」检查设置。
chinesesimp.WizardInstalling=正在安装
chinesesimp.WizardInstallingLabel=请稍候，正在安装 DAgents 本地助手…

[Files]
Source: "..\..\bundle\bin\*"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "dagents.cmd"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\bundle\scripts\*"; DestDir: "{app}\scripts"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "write-install-config.ps1"; DestDir: "{app}\scripts\windows"; Flags: ignoreversion
Source: "..\..\bundle\config.example.yaml"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\.env.example"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\README.txt"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\VERSION"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\.runtime\*"; DestDir: "{app}\.runtime"; Flags: recursesubdirs onlyifdoesntexist createallsubdirs; Excludes: "policy\*"
Source: "..\..\bundle\.runtime\policy\*"; DestDir: "{app}\.runtime\_seed\policy"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\DAgents Shell（系统托盘）"; Filename: "{app}\dagents.cmd"; Parameters: "shell --background"; WorkingDir: "{app}"
Name: "{group}\DAgents Shell"; Filename: "{cmd}"; Parameters: "/K cd /d ""{app}"" && dagents help"; WorkingDir: "{app}"
Name: "{group}\Start Agent Node (background)"; Filename: "{app}\dagents.cmd"; Parameters: "node"; WorkingDir: "{app}"
Name: "{group}\Start Agent Node (foreground)"; Filename: "{app}\dagents.cmd"; Parameters: "node --foreground"; WorkingDir: "{app}"
Name: "{group}\打开 Web UI"; Filename: "http://127.0.0.1:18765/ui/"

[Registry]

[Run]
Filename: "{app}\dagents.cmd"; Parameters: "doctor"; Description: "验证安装文件 (dagents doctor)"; Flags: postinstall skipifsilent runascurrentuser
Filename: "{app}\dagents.cmd"; Parameters: "shell --background"; Description: "启动 DAgents Shell（托盘监护 Node）"; Flags: postinstall nowait skipifsilent runascurrentuser

[Code]
const
  { Web UI tokens.css dark — Inno 颜色为 BGR $00BBGGRR }
  ClrBg = $00181818;
  ClrSurface = $001E1E1E;
  ClrSurfaceMuted = $00262626;
  ClrBorder = $002B2B2B;
  ClrText = $00CCCCCC;
  ClrTextMuted = $009D9D9D;
  ClrTextSubtle = $006E6E6E;
  ClrPrimary = $00FF9437;

var
  OverwritePolicy: Boolean;
  OverwritePolicyAnswered: Boolean;

procedure StyleStaticText(L: TNewStaticText; Title: Boolean);
begin
  L.Font.Name := 'Segoe UI';
  if Title then
  begin
    L.Font.Size := 12;
    L.Font.Style := [fsBold];
    L.Font.Color := ClrText;
  end
  else
  begin
    L.Font.Size := 9;
    L.Font.Style := [];
    L.Font.Color := ClrTextMuted;
  end;
end;

procedure StyleButton(B: TNewButton);
begin
  B.Font.Name := 'Segoe UI';
  B.Font.Size := 9;
end;

procedure ApplyWorkbenchTheme;
begin
  WizardForm.Color := ClrSurface;
  WizardForm.Font.Name := 'Segoe UI';
  WizardForm.Font.Size := 9;
  WizardForm.Font.Color := ClrText;

  if WizardForm.MainPanel <> nil then
    WizardForm.MainPanel.Color := ClrSurface;
  if WizardForm.InnerPage <> nil then
    WizardForm.InnerPage.Color := ClrSurface;
  if WizardForm.OuterNotebook <> nil then
    WizardForm.OuterNotebook.Color := ClrSurface;

  StyleStaticText(WizardForm.WelcomeLabel1, True);
  StyleStaticText(WizardForm.WelcomeLabel2, False);
  StyleStaticText(WizardForm.FinishedHeadingLabel, True);
  StyleStaticText(WizardForm.FinishedLabel, False);
  StyleStaticText(WizardForm.PageNameLabel, True);
  StyleStaticText(WizardForm.PageDescriptionLabel, False);

  StyleButton(WizardForm.NextButton);
  StyleButton(WizardForm.BackButton);
  StyleButton(WizardForm.CancelButton);
end;

function PolicyExistsAt(const AppDir: string): Boolean;
begin
  Result := FileExists(AppDir + '\.runtime\policy\tool.approval.txt');
end;

procedure CopyDefaultConfigIfMissing;
var
  AppDir, ExamplePath, ConfigPath: string;
begin
  AppDir := ExpandConstant('{app}');
  ExamplePath := AppDir + '\config.example.yaml';
  ConfigPath := AppDir + '\config.yaml';
  if FileExists(ConfigPath) then
    Exit;
  if not FileExists(ExamplePath) then
  begin
    MsgBox('缺少 config.example.yaml，请从安装目录手动复制为 config.yaml。', mbError, MB_OK);
    Exit;
  end;
  if not CopyFile(ExamplePath, ConfigPath, False) then
    MsgBox('无法创建 config.yaml，请手动复制 config.example.yaml。', mbError, MB_OK);
end;

procedure ApplyPolicySeed;
var
  AppDir, SeedDir, PolicyDir: string;
  ResultCode: Integer;
begin
  AppDir := ExpandConstant('{app}');
  SeedDir := AppDir + '\.runtime\_seed\policy';
  PolicyDir := AppDir + '\.runtime\policy';
  if not DirExists(SeedDir) then
    Exit;
  if OverwritePolicy or not PolicyExistsAt(AppDir) then
  begin
    ForceDirectories(PolicyDir);
    Exec(ExpandConstant('{cmd}'), '/C xcopy "' + SeedDir + '\*" "' + PolicyDir + '" /E /I /Y /Q',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
end;

procedure EnsureBrowserRuntimeDir;
var
  AppDir: string;
begin
  AppDir := ExpandConstant('{app}');
  ForceDirectories(AppDir + '\.runtime\browser');
  ForceDirectories(AppDir + '\.runtime\browser\profiles');
end;

procedure ShowPostInstallTips;
begin
  MsgBox(
    '安装完成。请打开 Web UI「设置 › 连接」配置 LLM、Manage 与功能开关。' + #13#10 +
    '真实 LLM 调用请在系统环境变量中设置 API Key（如 OPENAI_API_KEY）。' + #13#10 +
    'Desktop Shell 将随登录自启并监护 Node（dagents shell status）。',
    mbInformation, MB_OK);
end;

procedure InitializeWizard;
begin
  ApplyWorkbenchTheme;
  WizardForm.WelcomeLabel1.Caption := '欢迎安装 DAgents';
  WizardForm.WelcomeLabel2.Caption :=
    '本安装包包含 Agent Node、Desktop Shell（系统托盘）与 Client。' + #13#10 +
    '视觉与 Web UI Workbench 使用同一套深色主题；安装后可在浏览器打开本机助手。' + #13#10 +
    'LLM、Manage 与功能开关请在 Web UI「设置 › 连接」中完成配置。';
  WizardForm.FinishedLabel.Caption :=
    'DAgents 已就绪。建议立即打开 Web UI 完成连接配置，' + #13#10 +
    'Shell 将随登录自启并监护 Node（dagents shell status）。';
  WizardForm.FinishedHeadingLabel.Caption := '安装完成';
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  ApplyWorkbenchTheme;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  AppDir: string;
begin
  Result := True;
  if CurPageID <> wpSelectDir then
    Exit;
  if OverwritePolicyAnswered then
    Exit;
  AppDir := AddBackslash(WizardForm.DirEdit.Text);
  if not PolicyExistsAt(AppDir) then
  begin
    OverwritePolicy := False;
    OverwritePolicyAnswered := True;
    Exit;
  end;
  OverwritePolicy :=
    (MsgBox(
      '检测到已有 policy 配置（.runtime\policy）。' + #13#10 +
      '是否用安装包中的 policy 覆盖？' + #13#10#13#10 +
      '选「否」将保留现有 policy。',
      mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES);
  OverwritePolicyAnswered := True;
end;

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
  begin
    if FileExists(AppDir + '\bin\dagents-shell.exe') then
      Exec(AppDir + '\dagents.cmd', 'shell stop', AppDir, SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Exec(AppDir + '\dagents.cmd', 'node shutdown', AppDir, SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
end;

procedure InstallShellAutostart;
var
  AppDir, CmdLine: string;
begin
  AppDir := ExpandConstant('{app}');
  if not FileExists(AppDir + '\bin\dagents-shell.exe') then
    Exit;
  CmdLine := '"' + AppDir + '\dagents.cmd" shell --background';
  RegWriteStringValue(HKEY_CURRENT_USER, '{#ShellAutostartRegKey}', '{#ShellAutostartRunName}', CmdLine);
end;

procedure RemoveShellAutostart;
begin
  if RegValueExists(HKEY_CURRENT_USER, '{#ShellAutostartRegKey}', '{#ShellAutostartRunName}') then
    RegDeleteValue(HKEY_CURRENT_USER, '{#ShellAutostartRegKey}', '{#ShellAutostartRunName}');
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ResultCode: Integer;
  AppDir: string;
begin
  if CurUninstallStep = usUninstall then
  begin
    AppDir := ExpandConstant('{app}');
    if FileExists(AppDir + '\dagents.cmd') and FileExists(AppDir + '\bin\dagents-shell.exe') then
      Exec(AppDir + '\dagents.cmd', 'shell stop', AppDir, SW_HIDE, ewWaitUntilTerminated, ResultCode);
    RemoveShellAutostart;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Path, AppDir, ExternalToolsDir: string;
begin
  if CurStep = ssPostInstall then
  begin
    ApplyPolicySeed;
    CopyDefaultConfigIfMissing;
    EnsureBrowserRuntimeDir;
    InstallShellAutostart;
    ShowPostInstallTips;
  end;
  if CurStep <> ssPostInstall then
    Exit;
  AppDir := ExpandConstant('{app}');
  ExternalToolsDir := AppDir + '\.runtime\externaltools';
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
    Path := '';
  if not PathContains(Path, AppDir) then
    Path := Path + ';' + AppDir;
  if not PathContains(Path, ExternalToolsDir) then
    Path := Path + ';' + ExternalToolsDir;
  RegWriteExpandStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path);
end;
