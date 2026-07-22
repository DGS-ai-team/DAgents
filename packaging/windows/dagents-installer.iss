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
ShowLanguageDialog=no
SetupIconFile=..\..\desktop\tray\assets\icon.ico
WizardImageFile=assets\wizard-sidebar.bmp
WizardSmallImageFile=assets\wizard-small.bmp
WizardImageStretch=no
DisableWelcomePage=no
DisableFinishedPage=no

[Languages]
Name: "chinesesimp"; MessagesFile: "languages\ChineseSimplified.isl"

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
  { Web UI tokens.css light — Inno 颜色为 BGR $00BBGGRR }
  ClrBg = $00F8F6F5;
  ClrSurface = $00FFFFFF;
  ClrSurfaceMuted = $00FAF8F7;
  ClrBorder = $00EADED9;
  ClrText = $0030241F;
  ClrTextMuted = $0068554B;
  ClrTextSubtle = $008A776D;
  ClrPrimary = $00EB6325;

var
  OverwritePolicy: Boolean;
  OverwritePolicyAnswered: Boolean;

procedure StyleButton(B: TNewButton);
begin
  B.Font.Name := 'Segoe UI';
  B.Font.Size := 9;
end;

procedure StyleWelcomeLabels;
begin
  WizardForm.WelcomeLabel1.Font.Name := 'Segoe UI';
  WizardForm.WelcomeLabel1.Font.Size := 11;
  WizardForm.WelcomeLabel1.Font.Style := [fsBold];
  WizardForm.WelcomeLabel1.Font.Color := ClrText;
  WizardForm.WelcomeLabel1.AutoSize := False;

  WizardForm.WelcomeLabel2.Font.Name := 'Segoe UI';
  WizardForm.WelcomeLabel2.Font.Size := 9;
  WizardForm.WelcomeLabel2.Font.Style := [];
  WizardForm.WelcomeLabel2.Font.Color := ClrTextMuted;
  WizardForm.WelcomeLabel2.AutoSize := False;
  WizardForm.WelcomeLabel2.WordWrap := True;
  WizardForm.WelcomeLabel2.Width := WizardForm.ClientWidth - ScaleX(220);
end;

procedure StyleFinishedLabels;
begin
  WizardForm.FinishedHeadingLabel.Font.Name := 'Segoe UI';
  WizardForm.FinishedHeadingLabel.Font.Size := 11;
  WizardForm.FinishedHeadingLabel.Font.Style := [fsBold];
  WizardForm.FinishedHeadingLabel.Font.Color := ClrText;

  WizardForm.FinishedLabel.Font.Name := 'Segoe UI';
  WizardForm.FinishedLabel.Font.Size := 9;
  WizardForm.FinishedLabel.Font.Style := [];
  WizardForm.FinishedLabel.Font.Color := ClrTextMuted;
  WizardForm.FinishedLabel.AutoSize := False;
  WizardForm.FinishedLabel.WordWrap := True;
  WizardForm.FinishedLabel.Width := WizardForm.ClientWidth - ScaleX(220);
end;

procedure StyleInnerPageLabels;
var
  DescTop: Integer;
begin
  WizardForm.PageNameLabel.Font.Name := 'Segoe UI';
  WizardForm.PageNameLabel.Font.Size := 10;
  WizardForm.PageNameLabel.Font.Style := [fsBold];
  WizardForm.PageNameLabel.Font.Color := ClrText;
  WizardForm.PageNameLabel.AutoSize := True;

  WizardForm.PageDescriptionLabel.Font.Name := 'Segoe UI';
  WizardForm.PageDescriptionLabel.Font.Size := 9;
  WizardForm.PageDescriptionLabel.Font.Style := [];
  WizardForm.PageDescriptionLabel.Font.Color := ClrTextMuted;
  WizardForm.PageDescriptionLabel.AutoSize := False;
  WizardForm.PageDescriptionLabel.WordWrap := True;
  WizardForm.PageDescriptionLabel.Left := WizardForm.PageNameLabel.Left;
  WizardForm.PageDescriptionLabel.Width := WizardForm.ClientWidth - ScaleX(220);
  DescTop := WizardForm.PageNameLabel.Top + WizardForm.PageNameLabel.Height + ScaleY(6);
  if DescTop > WizardForm.PageDescriptionLabel.Top then
    WizardForm.PageDescriptionLabel.Top := DescTop;
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

  StyleWelcomeLabels;
  StyleFinishedLabels;
  StyleInnerPageLabels;

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

procedure InitializeWizard;
begin
  ApplyWorkbenchTheme;
  WizardForm.WelcomeLabel1.Caption := '欢迎安装 DAgents';
  WizardForm.WelcomeLabel2.Caption :=
    '本安装包包含 Agent Node、Desktop Shell（系统托盘）与 Client。' + #13#10 +
    '界面与 Web UI 浅色主题一致；安装后可在浏览器打开本机助手。' + #13#10 +
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
