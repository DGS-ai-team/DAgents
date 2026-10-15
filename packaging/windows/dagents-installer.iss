#define MyAppName "DAgents 本地助手"
#ifdef IsolatedQaBuild
#define MyQaAppName "DAgents QA 本地助手"
#define MyAppId "{{B5D1A7C3-2E49-4F60-A8B1-7C3D9E5F2046}"
#define MyDefaultDir "{localappdata}\DAgents-QA"
#else
#define MyAppId "{{A3B8C2D1-9E4F-4A7B-8C6D-1E2F3A4B5C6D}"
#define MyDefaultDir "{userpf}\DAgents"
#endif
#ifndef MyAppVersion
#define MyAppVersion "0.0.0"
#endif
#ifndef MyAppArch
#define MyAppArch "x64"
#endif
#ifndef MyOutputBaseFilename
#ifdef IsolatedQaBuild
#define MyOutputBaseFilename "dagents-local-assistant-windows-qa-installer"
#else
#define MyOutputBaseFilename "dagents-local-assistant-windows-amd64-installer"
#endif
#endif
#define ShellAutostartRunName "DAgents Shell"
#define ShellAutostartRegKey "Software\Microsoft\Windows\CurrentVersion\Run"

[Setup]
AppId={#MyAppId}
#ifdef IsolatedQaBuild
AppName={#MyQaAppName}
#else
AppName={#MyAppName}
#endif
AppVersion={#MyAppVersion}
AppPublisher=DAgents
AppPublisherURL=https://github.com/DGS-ai-team/DAgents
; Node keeps mutable config and runtime data under its working directory, so
; install in the current user's Programs directory.
DefaultDirName={#MyDefaultDir}
#ifdef IsolatedQaBuild
DefaultGroupName={#MyQaAppName}
#else
DefaultGroupName=DAgents
#endif
DisableProgramGroupPage=yes
OutputDir=..\..\dist-installer
OutputBaseFilename={#MyOutputBaseFilename}
Compression=lzma2
SolidCompression=yes
CloseApplications=no
RestartApplications=no
#if MyAppArch == "x64"
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#else
ArchitecturesAllowed=x86compatible
#endif
PrivilegesRequired=lowest
UninstallDisplayIcon={app}\assets\dagents.ico
ChangesEnvironment=yes
WizardStyle=modern
ShowLanguageDialog=no
SetupIconFile=..\..\desktop\tray-tauri\src-tauri\icons\icon.ico
WizardImageFile=assets\wizard-sidebar.bmp
WizardSmallImageFile=assets\wizard-small.bmp
WizardImageStretch=yes
DisableWelcomePage=no
DisableFinishedPage=no

[Languages]
Name: "chinesesimp"; MessagesFile: "languages\ChineseSimplified.isl"

[CustomMessages]
chinesesimp.WelcomeLabel2=安装完成后请打开 DAgents Desktop 完成配置。
chinesesimp.WizardSelectDir=选择安装位置
chinesesimp.WizardSelectDirLabel3=DAgents 将安装到以下文件夹。
chinesesimp.WizardReady=准备安装
chinesesimp.WizardReadyLabel1=已准备好安装 DAgents 本地助手。
chinesesimp.WizardReadyLabel2a=点击「安装」开始，或返回上一步检查设置。
chinesesimp.WizardInstalling=正在安装
chinesesimp.WizardInstallingLabel=请稍候，正在安装 DAgents…

[Files]
; 安装包只发布独立 Desktop Shell（Tauri）。
Source: "..\..\bundle\bin\*"; DestDir: "{app}\bin"; Flags: ignoreversion; Excludes: "dagents-shell-tauri.exe"
Source: "..\..\bundle\bin\dagents-shell-tauri.exe"; DestDir: "{app}\bin"; DestName: "dagents-shell.exe"; Flags: ignoreversion
Source: "dagents.cmd"; DestDir: "{app}"; Flags: ignoreversion
Source: "lifecycle-protocol-v1.marker"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\desktop\tray-tauri\src-tauri\icons\icon.ico"; DestDir: "{app}\assets"; DestName: "dagents.ico"; Flags: ignoreversion
Source: "..\..\bundle\scripts\*"; DestDir: "{app}\scripts"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "write-install-config.ps1"; DestDir: "{app}\scripts\windows"; Flags: ignoreversion
Source: "..\..\bundle\config.example.yaml"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\.env.example"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\README.txt"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\VERSION"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\bundle\.runtime\*"; DestDir: "{app}\.runtime"; Flags: recursesubdirs onlyifdoesntexist createallsubdirs uninsneveruninstall; Excludes: "policy\*"
Source: "..\..\bundle\.runtime\policy\*"; DestDir: "{app}\.runtime\_seed\policy"; Flags: ignoreversion recursesubdirs createallsubdirs uninsneveruninstall
Source: "..\..\bundle\packaging\agent-templates\*"; DestDir: "{app}\packaging\agent-templates"; Flags: ignoreversion recursesubdirs createallsubdirs skipifsourcedoesntexist

#ifndef IsolatedQaBuild
[Icons]
Name: "{group}\DAgents Desktop（系统托盘）"; Filename: "{app}\dagents.cmd"; Parameters: "shell --background"; WorkingDir: "{app}"; IconFilename: "{app}\assets\dagents.ico"
Name: "{group}\DAgents Command Prompt"; Filename: "{cmd}"; Parameters: "/K cd /d ""{app}"" && dagents help"; WorkingDir: "{app}"; IconFilename: "{app}\assets\dagents.ico"
Name: "{group}\Start Agent Node (background)"; Filename: "{app}\dagents.cmd"; Parameters: "node --background"; WorkingDir: "{app}"; IconFilename: "{app}\assets\dagents.ico"
Name: "{group}\Start Agent Node (foreground)"; Filename: "{app}\dagents.cmd"; Parameters: "node --foreground"; WorkingDir: "{app}"; IconFilename: "{app}\assets\dagents.ico"
#endif

[Registry]

#ifndef IsolatedQaBuild
[Run]
Filename: "{app}\dagents.cmd"; Parameters: "doctor"; Description: "验证安装文件 (dagents doctor)"; Flags: postinstall skipifsilent runascurrentuser
; Silent upgrades launched by the Shell must start the new Shell after replacement.
Filename: "{app}\dagents.cmd"; Parameters: "shell --background"; Description: "启动 DAgents Desktop（系统托盘）"; Flags: postinstall nowait runascurrentuser
#endif

#ifndef IsolatedQaBuild
[UninstallDelete]
Type: files; Name: "{userdesktop}\DAgents Desktop（系统托盘）.lnk"
#endif

[Code]
const
  { Desktop UI theme — Inno 颜色为 BGR $00BBGGRR }
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
  DesktopShortcutCheck: TNewCheckBox;
  DesktopShortcutCreated: Boolean;

function WebView2VersionIsInstalled(const VersionText: string): Boolean;
var
  PackedVersion: Int64;
begin
  Result := StrToVersion(VersionText, PackedVersion) and (PackedVersion > 0);
end;

function WebView2RuntimeInstalled: Boolean;
var
  Version: string;
begin
  Result := False;
  if RegQueryStringValue(HKEY_LOCAL_MACHINE_32,
       'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}',
       'pv', Version) and WebView2VersionIsInstalled(Version) then
    Result := True;
  if not Result and RegQueryStringValue(HKEY_CURRENT_USER_32,
       'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}',
       'pv', Version) and WebView2VersionIsInstalled(Version) then
    Result := True;
  if not Result and IsWin64 then
  begin
    if RegQueryStringValue(HKEY_LOCAL_MACHINE_64,
         'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}',
         'pv', Version) and WebView2VersionIsInstalled(Version) then
      Result := True;
    if not Result and RegQueryStringValue(HKEY_CURRENT_USER_64,
         'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}',
         'pv', Version) and WebView2VersionIsInstalled(Version) then
      Result := True;
  end;
end;

function SupportedWindowsVersion: Boolean;
var
  Version: TWindowsVersion;
begin
  Result := False;
  GetWindowsVersionEx(Version);
  if not Version.NTPlatform then
    Exit;

  { Workstations: Windows 10 (10.0.10240) is the Node baseline. Windows 11
    (build 22000+) is recommended for the full Desktop experience. }
  if Version.ProductType = VER_NT_WORKSTATION then
    Result := (Version.Major > 10) or
      ((Version.Major = 10) and ((Version.Minor > 0) or
       ((Version.Minor = 0) and (Version.Build >= 10240))))
  else
    { Windows Server 2019 is 10.0.17763; older Server releases are unsupported. }
    Result := (Version.Major > 10) or
      ((Version.Major = 10) and ((Version.Minor > 0) or
       ((Version.Minor = 0) and (Version.Build >= 17763))));
end;

function InitializeSetup: Boolean;
begin
  Result := True;
  if not SupportedWindowsVersion then
  begin
    MsgBox(
      '此 DAgents 版本要求 Windows 10 或更新版本；Windows Server 需为 Server 2019 或更新版本。' + #13#10 + #13#10 +
      '完整 Desktop 体验推荐 Windows 11。Windows 7/8/8.1 和旧版 Windows Server 不受支持。',
      mbError, MB_OK);
    Result := False;
    Exit;
  end;
  if not WebView2RuntimeInstalled then
  begin
    MsgBox(
      'DAgents Desktop 需要 Microsoft Edge WebView2 Runtime。' + #13#10 + #13#10 +
      '请先安装 Evergreen Runtime，然后重新运行 DAgents 安装程序：' + #13#10 +
      'https://developer.microsoft.com/microsoft-edge/webview2/',
      mbError, MB_OK);
    Result := False;
  end;
end;

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
  ContentLeft, ContentWidth, BodyTop, FinishedHeight, CheckTop: Integer;
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
  FinishedHeight := ScaleY(100);
  if WizardForm.RunList.Visible and (WizardForm.RunList.Top > BodyTop) then
  begin
    FinishedHeight := WizardForm.RunList.Top - BodyTop - ScaleY(38);
    if FinishedHeight > ScaleY(100) then
      FinishedHeight := ScaleY(100);
  end;
  if FinishedHeight < ScaleY(70) then
    FinishedHeight := ScaleY(70);
  WizardForm.FinishedLabel.Height := FinishedHeight;

  if DesktopShortcutCheck <> nil then
  begin
    DesktopShortcutCheck.Font.Name := 'Segoe UI';
    DesktopShortcutCheck.Font.Size := 9;
    DesktopShortcutCheck.Font.Color := ClrText;
    DesktopShortcutCheck.Left := ContentLeft;
    DesktopShortcutCheck.Width := ContentWidth;
    DesktopShortcutCheck.Height := ScaleY(22);
    CheckTop := BodyTop + FinishedHeight + ScaleY(8);
    if WizardForm.RunList.Visible and
      ((CheckTop + DesktopShortcutCheck.Height) > WizardForm.RunList.Top) then
      CheckTop := WizardForm.RunList.Top - DesktopShortcutCheck.Height - ScaleY(8);
    DesktopShortcutCheck.Top := CheckTop;
  end;
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

procedure InitializeWizard;
begin
  DesktopShortcutCreated := False;
#ifndef IsolatedQaBuild
  DesktopShortcutCheck := TNewCheckBox.Create(WizardForm);
  DesktopShortcutCheck.Parent := WizardForm.FinishedPage;
  DesktopShortcutCheck.Caption := '创建 DAgents Desktop 桌面快捷方式';
  DesktopShortcutCheck.Checked := False;
#endif
  ApplyWorkbenchTheme;
  WizardForm.WelcomeLabel1.Caption := '欢迎安装 DAgents';
  WizardForm.WelcomeLabel2.Caption :=
    'DAgents 安装包包含：' + #13#10 + #13#10 +
    '  ·  Agent Node（本地运行时）' + #13#10 +
    '  ·  DAgents Desktop（独立桌面应用）' + #13#10 +
    '' + #13#10 +
    '安装完成后，请打开 DAgents Desktop 配置本机 Node、LLM 与 Manage。';
  WizardForm.FinishedHeadingLabel.Caption := '安装完成';
#ifdef IsolatedQaBuild
  WizardForm.FinishedLabel.Caption :=
    'DAgents QA 安装已就绪。此构建仅用于隔离验证；Desktop 设置保存在安装目录内。';
#else
  WizardForm.FinishedLabel.Caption :=
    'DAgents 已就绪。' + #13#10 + #13#10 +
    '  ·  打开 DAgents Desktop 完成配置' + #13#10 +
    '  ·  Desktop 将随登录自启并管理本机 Node' + #13#10 + #13#10 +
    '可用 dagents shell status 查看 Desktop 状态。';
#endif
  StyleFinishedLabels;
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  ApplyWorkbenchTheme;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  AppDir, ShortcutPath, CreatedLink: string;
begin
  Result := True;
  if CurPageID = wpFinished then
  begin
#ifndef IsolatedQaBuild
    if (DesktopShortcutCheck <> nil) and DesktopShortcutCheck.Checked and
      (not DesktopShortcutCreated) then
    begin
      ShortcutPath := ExpandConstant('{userdesktop}\DAgents Desktop（系统托盘）.lnk');
      try
        CreatedLink := CreateShellLink(
          ShortcutPath,
          'DAgents Desktop（系统托盘）',
          ExpandConstant('{app}\dagents.cmd'),
          'shell --background',
          ExpandConstant('{app}'),
          ExpandConstant('{app}\assets\dagents.ico'),
          0,
          SW_SHOWNORMAL);
        DesktopShortcutCreated := CreatedLink <> '';
      except
        MsgBox('无法创建桌面快捷方式，请稍后手动创建。', mbError, MB_OK);
      end;
    end;
#endif
    Exit;
  end;
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

function IsUnderDirectory(const Path, Dir: string): Boolean;
begin
  Result := Pos(Uppercase(AddBackslash(Dir)), Uppercase(AddBackslash(Path))) = 1;
end;

function RunShellCommand(const AppDir, CommandLine: string): Integer;
var
  ShellExe, Params: string;
begin
  ShellExe := AppDir + '\bin\dagents-shell.exe';
  if not FileExists(ShellExe) then
  begin
    Result := -1;
    Exit;
  end;
  Params := CommandLine;
  if not Exec(ShellExe, Params, AppDir, SW_HIDE, ewWaitUntilTerminated, Result) then
    Result := -1;
end;

function DesktopConfigArgument(const AppDir: string): string;
begin
#ifdef IsolatedQaBuild
  Result := ' --desktop-config "' + AppDir + '\qa-desktop-settings.json"';
#else
  Result := '';
#endif
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
  AppDir: string;
  ProbePath: string;
  ProbeSource: string;
  ProbeIndex: Integer;
  ProbeCreated: Boolean;
begin
  Result := '';
  AppDir := ExpandConstant('{app}');
  if IsUnderDirectory(AppDir, ExpandConstant('{commonpf}')) or
     IsUnderDirectory(AppDir, ExpandConstant('{commonpf32}')) then
  begin
    Result := 'DAgents 当前只支持按当前用户安装。检测到所选目录位于 Program Files：' + #13#10 +
      AppDir + #13#10#13#10 +
      '安装器不会移动或覆盖这个目录中的 Node 配置和运行数据。请先为该旧目录备份数据，再选择用户可写的安装目录。';
    Exit;
  end;
  if not ForceDirectories(AppDir) then
  begin
    Result := '无法创建安装目录，请选择当前用户可写的位置：' + #13#10 + AppDir;
    Exit;
  end;
  ProbeSource := GenerateUniqueName(ExpandConstant('{tmp}'), '.tmp');
  if not SaveStringToFile(ProbeSource, 'DAgents install write check', False) then
  begin
    Result := '无法创建安装目录权限检查文件。';
    Exit;
  end;
  ProbeCreated := False;
  ProbePath := '';
  for ProbeIndex := 0 to 99 do
  begin
    ProbePath := AddBackslash(AppDir) + '.dagents-write-test-' + IntToStr(ProbeIndex) + '.tmp';
    if CopyFile(ProbeSource, ProbePath, True) then
    begin
      ProbeCreated := True;
      Break;
    end;
    if not FileExists(ProbePath) then
      Break;
  end;
  DeleteFile(ProbeSource);
  if not ProbeCreated then
  begin
    Result := '当前用户无法写入所选安装目录，请选择一个可写的位置：' + #13#10 + AppDir;
    Exit;
  end;
  DeleteFile(ProbePath);
  if FileExists(AppDir + '\bin\dagents-shell.exe') then
  begin
    if not FileExists(AppDir + '\lifecycle-protocol-v1.marker') then
    begin
      Result := '此目录中的 DAgents 版本不支持安全升级协议。安装已取消，且未调用旧 Shell。' + #13#10#13#10 +
        '请先在旧版 Desktop 中退出并备份数据，再使用旧版本自己的卸载程序，然后选择新的用户可写目录安装。安装器不会自动迁移数据。';
      Exit;
    end;
    ResultCode := RunShellCommand(AppDir, '--quit-desktop --config "' + AppDir + '\config.yaml"' +
      DesktopConfigArgument(AppDir));
    if ResultCode <> 0 then
    begin
      Result := '无法安全退出此安装中的 DAgents Desktop，升级已取消。' + #13#10#13#10 +
        '请先在 Desktop 中选择退出，然后重试。';
      Exit;
    end;
  end;
  if FileExists(AppDir + '\bin\dagents-node.exe') then
  begin
    ResultCode := RunShellCommand(AppDir, '--stop-owned-node --require-idle --config "' + AppDir + '\config.yaml"' +
      DesktopConfigArgument(AppDir));
    if ResultCode <> 0 then
    begin
      Result := '无法验证并停止此安装管理的 Agent Node。为避免覆盖仍在运行的 Node，升级已取消。' + #13#10#13#10 +
        '请确认 Node 空闲且由此 Desktop 管理，然后重试。';
      Exit;
    end;
  end
  else if FileExists(AppDir + '\bin\dagents-shell.exe') then
  begin
    ResultCode := RunShellCommand(AppDir, '--stop-owned-node --require-idle --config "' + AppDir + '\config.yaml"' +
      DesktopConfigArgument(AppDir));
    if ResultCode <> 0 then
    begin
      Result := '无法验证此安装是否仍有受管 Node 运行，升级已取消。';
      Exit;
    end;
  end;
end;

#ifndef IsolatedQaBuild
function PathContains(const Path, Dir: string): Boolean;
begin
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') <> 0;
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
var
  CurrentCommand, OwnedCommand, AppDir: string;
begin
  AppDir := ExpandConstant('{app}');
  OwnedCommand := '"' + AppDir + '\dagents.cmd" shell --background';
  if RegQueryStringValue(HKEY_CURRENT_USER, '{#ShellAutostartRegKey}', '{#ShellAutostartRunName}', CurrentCommand) and
     SameText(CurrentCommand, OwnedCommand) then
    RegDeleteValue(HKEY_CURRENT_USER, '{#ShellAutostartRegKey}', '{#ShellAutostartRunName}');
end;
#endif

#ifndef IsolatedQaBuild
function RemovePathEntry(const Path, Dir: string): string;
var
  Remaining, Entry: string;
  Separator: Integer;
begin
  Result := '';
  Remaining := Path;
  while True do
  begin
    Separator := Pos(';', Remaining);
    if Separator = 0 then
      Entry := Remaining
    else
    begin
      Entry := Copy(Remaining, 1, Separator - 1);
      Delete(Remaining, 1, Separator);
    end;
    if (Entry <> '') and not SameText(RemoveBackslash(Entry), RemoveBackslash(Dir)) then
    begin
      if Result <> '' then
        Result := Result + ';';
      Result := Result + Entry;
    end;
    if Separator = 0 then
      Break;
  end;
end;
#endif

function InitializeUninstall: Boolean;
var
  AppDir: string;
  ResultCode: Integer;
begin
  Result := True;
  AppDir := ExpandConstant('{app}');
  if FileExists(AppDir + '\bin\dagents-shell.exe') then
  begin
    if not FileExists(AppDir + '\lifecycle-protocol-v1.marker') then
    begin
      MsgBox(
        '此目录中的 DAgents 版本不支持安全卸载协议，卸载已取消。请先在旧版 Desktop 中退出并备份数据，再使用旧版本自己的卸载程序。',
        mbError, MB_OK);
      Result := False;
      Exit;
    end;
    ResultCode := RunShellCommand(AppDir, '--quit-desktop --config "' + AppDir + '\config.yaml"' +
      DesktopConfigArgument(AppDir));
    if ResultCode <> 0 then
    begin
      MsgBox(
        '无法安全退出此安装中的 DAgents Desktop。卸载已取消。' + #13#10#13#10 +
        '请先在 Desktop 中选择退出，然后重试。',
        mbError, MB_OK);
      Result := False;
      Exit;
    end;
  end;
  if FileExists(AppDir + '\bin\dagents-node.exe') then
  begin
    ResultCode := RunShellCommand(AppDir, '--stop-owned-node --require-idle --config "' + AppDir + '\config.yaml"' +
      DesktopConfigArgument(AppDir));
    if ResultCode <> 0 then
    begin
      MsgBox(
        '无法验证并停止此安装管理的 Agent Node。为避免误停外部 Node，卸载已取消。' + #13#10#13#10 +
        '请确认 Node 空闲且由此 Desktop 管理，然后重试。',
        mbError, MB_OK);
      Result := False;
      Exit;
    end;
  end
  else if FileExists(AppDir + '\bin\dagents-shell.exe') then
  begin
    ResultCode := RunShellCommand(AppDir, '--stop-owned-node --require-idle --config "' + AppDir + '\config.yaml"' +
      DesktopConfigArgument(AppDir));
    if ResultCode <> 0 then
    begin
      MsgBox('无法验证此安装是否仍有受管 Node 运行。卸载已取消。', mbError, MB_OK);
      Result := False;
      Exit;
    end;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Path, AppDir: string;
begin
#ifndef IsolatedQaBuild
  if CurUninstallStep = usUninstall then
  begin
    RemoveShellAutostart;
    AppDir := ExpandConstant('{app}');
    if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
      Path := '';
    Path := RemovePathEntry(Path, AppDir);
    Path := RemovePathEntry(Path, AppDir + '\.runtime\externaltools');
    RegWriteExpandStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path);
  end;
#endif
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
#ifndef IsolatedQaBuild
    InstallShellAutostart;
#endif
  end;
  if CurStep <> ssPostInstall then
    Exit;
#ifndef IsolatedQaBuild
  AppDir := ExpandConstant('{app}');
  ExternalToolsDir := AppDir + '\.runtime\externaltools';
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
    Path := '';
  if not PathContains(Path, AppDir) then
    Path := Path + ';' + AppDir;
  if not PathContains(Path, ExternalToolsDir) then
    Path := Path + ';' + ExternalToolsDir;
  RegWriteExpandStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path);
#endif
end;
