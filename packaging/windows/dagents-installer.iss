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
chinesesimp.WelcomeLabel2=安装完成后请打开 Web UI「设置 › 连接」完成配置。
chinesesimp.WizardSelectDir=选择安装位置
chinesesimp.WizardSelectDirLabel3=DAgents 将安装到以下文件夹。
chinesesimp.WizardSelectTasks=选择 Shell
chinesesimp.WizardSelectTasksLabel2=Desktop Shell 二选一；有 WebView2 时推荐内嵌 Web UI。
chinesesimp.WizardReady=准备安装
chinesesimp.WizardReadyLabel1=已准备好安装 DAgents 本地助手。
chinesesimp.WizardReadyLabel2a=点击「安装」开始，或返回上一步检查设置。
chinesesimp.WizardInstalling=正在安装
chinesesimp.WizardInstallingLabel=请稍候，正在安装 DAgents…
chinesesimp.ShellGroup=Desktop Shell
chinesesimp.ShellModernTask=推荐 · Win10/11 + WebView2（内嵌 Web UI）
chinesesimp.ShellLegacyTask=兼容 · 低版本 Windows（系统浏览器打开 Web UI）

[Tasks]
Name: "shellmodern"; Description: "{cm:ShellModernTask}"; GroupDescription: "{cm:ShellGroup}"; Flags: exclusive
Name: "shelllegacy"; Description: "{cm:ShellLegacyTask}"; GroupDescription: "{cm:ShellGroup}"; Flags: exclusive unchecked

[Files]
; 核心二进制：排除两套 Shell，按 Tasks 二选一写成 bin\dagents-shell.exe
Source: "..\..\bundle\bin\*"; DestDir: "{app}\bin"; Flags: ignoreversion; Excludes: "dagents-shell.exe;dagents-shell-tauri.exe;dagents-shell-legacy.exe"
Source: "..\..\bundle\bin\dagents-shell-tauri.exe"; DestDir: "{app}\bin"; DestName: "dagents-shell.exe"; Flags: ignoreversion; Tasks: shellmodern
Source: "..\..\bundle\bin\dagents-shell-legacy.exe"; DestDir: "{app}\bin"; DestName: "dagents-shell.exe"; Flags: ignoreversion; Tasks: shelllegacy
Source: "dagents.cmd"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\bundle\scripts\*"; DestDir: "{app}\scripts"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "write-install-config.ps1"; DestDir: "{app}\scripts\windows"; Flags: ignoreversion
Source: "..\..\bundle\config.example.yaml"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\.env.example"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\README.txt"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\VERSION"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\.runtime\*"; DestDir: "{app}\.runtime"; Flags: recursesubdirs onlyifdoesntexist createallsubdirs; Excludes: "policy\*"
Source: "..\..\bundle\.runtime\policy\*"; DestDir: "{app}\.runtime\_seed\policy"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\..\bundle\packaging\agent-templates\*"; DestDir: "{app}\packaging\agent-templates"; Flags: ignoreversion recursesubdirs createallsubdirs skipifsourcedoesntexist

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
  ClrBg = $00F3F3F3;
  ClrSurface = $00FFFFFF;
  ClrSurfaceMuted = $00F6F6F6;
  ClrBorder = $00E4E4E4;
  ClrText = $001A1A1A;
  ClrTextMuted = $005D5D5D;
  ClrTextSubtle = $008A8A8A;
  ClrPrimary = $00D47800;

var
  OverwritePolicy: Boolean;
  OverwritePolicyAnswered: Boolean;
  ShellTaskDefaultsApplied: Boolean;

procedure StyleButton(B: TNewButton);
begin
  B.Font.Name := 'Segoe UI';
  B.Font.Size := 9;
  B.Font.Color := ClrText;
end;

procedure StyleWelcomeLabels;
var
  ContentLeft, ContentWidth, BodyTop: Integer;
begin
  ContentLeft := WizardForm.WelcomeLabel1.Left;
  ContentWidth := WizardForm.ClientWidth - ContentLeft - ScaleX(28);

  WizardForm.WelcomeLabel1.Font.Name := 'Segoe UI';
  WizardForm.WelcomeLabel1.Font.Size := 14;
  WizardForm.WelcomeLabel1.Font.Style := [fsBold];
  WizardForm.WelcomeLabel1.Font.Color := ClrText;
  WizardForm.WelcomeLabel1.AutoSize := False;
  WizardForm.WelcomeLabel1.Width := ContentWidth;
  WizardForm.WelcomeLabel1.Height := ScaleY(28);

  BodyTop := WizardForm.WelcomeLabel1.Top + WizardForm.WelcomeLabel1.Height + ScaleY(14);
  WizardForm.WelcomeLabel2.Font.Name := 'Segoe UI';
  WizardForm.WelcomeLabel2.Font.Size := 9;
  WizardForm.WelcomeLabel2.Font.Style := [];
  WizardForm.WelcomeLabel2.Font.Color := ClrTextMuted;
  WizardForm.WelcomeLabel2.AutoSize := False;
  WizardForm.WelcomeLabel2.WordWrap := True;
  WizardForm.WelcomeLabel2.Left := ContentLeft;
  WizardForm.WelcomeLabel2.Top := BodyTop;
  WizardForm.WelcomeLabel2.Width := ContentWidth;
  WizardForm.WelcomeLabel2.Height := ScaleY(170);
end;

procedure StyleFinishedLabels;
var
  ContentLeft, ContentWidth, BodyTop: Integer;
begin
  ContentLeft := WizardForm.FinishedHeadingLabel.Left;
  ContentWidth := WizardForm.ClientWidth - ContentLeft - ScaleX(28);

  WizardForm.FinishedHeadingLabel.Font.Name := 'Segoe UI';
  WizardForm.FinishedHeadingLabel.Font.Size := 14;
  WizardForm.FinishedHeadingLabel.Font.Style := [fsBold];
  WizardForm.FinishedHeadingLabel.Font.Color := ClrText;
  WizardForm.FinishedHeadingLabel.AutoSize := False;
  WizardForm.FinishedHeadingLabel.Width := ContentWidth;
  WizardForm.FinishedHeadingLabel.Height := ScaleY(28);

  BodyTop := WizardForm.FinishedHeadingLabel.Top + WizardForm.FinishedHeadingLabel.Height + ScaleY(14);
  WizardForm.FinishedLabel.Font.Name := 'Segoe UI';
  WizardForm.FinishedLabel.Font.Size := 9;
  WizardForm.FinishedLabel.Font.Style := [];
  WizardForm.FinishedLabel.Font.Color := ClrTextMuted;
  WizardForm.FinishedLabel.AutoSize := False;
  WizardForm.FinishedLabel.WordWrap := True;
  WizardForm.FinishedLabel.Left := ContentLeft;
  WizardForm.FinishedLabel.Top := BodyTop;
  WizardForm.FinishedLabel.Width := ContentWidth;
  WizardForm.FinishedLabel.Height := ScaleY(120);
end;

procedure StyleInnerPageLabels;
var
  DescTop: Integer;
begin
  WizardForm.PageNameLabel.Font.Name := 'Segoe UI';
  WizardForm.PageNameLabel.Font.Size := 11;
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
  DescTop := WizardForm.PageNameLabel.Top + WizardForm.PageNameLabel.Height + ScaleY(8);
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

function IsWebView2Installed: Boolean;
begin
  { Evergreen WebView2 Runtime client id }
  Result :=
    RegKeyExists(HKLM, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}') or
    RegKeyExists(HKLM, 'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}') or
    RegKeyExists(HKCU, 'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}');
end;

procedure InitializeWizard;
begin
  ShellTaskDefaultsApplied := False;
  ApplyWorkbenchTheme;
  WizardForm.WelcomeLabel1.Caption := '欢迎安装 DAgents';
  WizardForm.WelcomeLabel2.Caption :=
    '本机智能助手安装包包含：' + #13#10 + #13#10 +
    '  ·  Agent Node（本地运行时）' + #13#10 +
    '  ·  Desktop Shell（系统托盘）' + #13#10 +
    '  ·  Client' + #13#10 + #13#10 +
    '下一步可选择 Shell 类型。' + #13#10 +
    '安装完成后，请在 Web UI「设置 › 连接」配置 LLM 与 Manage。';
  WizardForm.FinishedHeadingLabel.Caption := '安装完成';
  WizardForm.FinishedLabel.Caption :=
    'DAgents 已就绪。' + #13#10 + #13#10 +
    '  ·  打开 Web UI 完成「设置 › 连接」' + #13#10 +
    '  ·  Shell 将随登录自启并监护 Node' + #13#10 + #13#10 +
    '可用 dagents shell status 查看托盘状态。';
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  ApplyWorkbenchTheme;
  if (CurPageID = wpSelectTasks) and (not ShellTaskDefaultsApplied) then
  begin
    ShellTaskDefaultsApplied := True;
    if IsWebView2Installed then
      WizardSelectTasks('shellmodern')
    else
      WizardSelectTasks('shelllegacy');
  end;
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
