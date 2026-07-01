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
SetupIconFile=compiler:SetupClassicIcon.ico

[Languages]
Name: "chinesesimp"; MessagesFile: "Languages\ChineseSimplified.isl"

[CustomMessages]
chinesesimp.WelcomeLabel2=本向导将分三批引导您完成 LLM、Manage 与功能开关配置，并生成 config.yaml。%n%n安装后请在系统环境变量中设置 API Key（如 OPENAI_API_KEY）。

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
Name: "{group}\DAgents Shell"; Filename: "{cmd}"; Parameters: "/K cd /d ""{app}"" && dagents help"; WorkingDir: "{app}"
Name: "{group}\Start Agent Node (background)"; Filename: "{app}\dagents.cmd"; Parameters: "node"; WorkingDir: "{app}"
Name: "{group}\Start Agent Node (foreground)"; Filename: "{app}\dagents.cmd"; Parameters: "node --foreground"; WorkingDir: "{app}"
Name: "{group}\Chat (Textual TUI)"; Filename: "{app}\dagents.cmd"; Parameters: "chat --withnode"; WorkingDir: "{app}"
Name: "{group}\TUI (Go full-screen)"; Filename: "{app}\dagents.cmd"; Parameters: "tui --withnode"; WorkingDir: "{app}"
Name: "{group}\REPL (Go line mode)"; Filename: "{app}\dagents.cmd"; Parameters: "tui --withnode --plain"; WorkingDir: "{app}"
Name: "{group}\打开 Web UI"; Filename: "http://127.0.0.1:18765/ui/"

[Registry]

[Run]
Filename: "{app}\dagents.cmd"; Parameters: "doctor"; Description: "验证安装文件 (dagents doctor)"; Flags: postinstall skipifsilent runascurrentuser

[Code]
var
  OverwritePolicy: Boolean;
  OverwritePolicyAnswered: Boolean;
  LLMProviderPage: TWizardPage;
  LLMProviderCombo: TNewComboBox;
  LLMDetailPage: TInputQueryWizardPage;
  ManageEnablePage: TWizardPage;
  ManageEnableCheck: TNewCheckBox;
  ManageDetailPage: TInputQueryWizardPage;
  FeaturesPage: TWizardPage;
  FeatureSkills, FeatureTriggers, FeatureChildAgents, FeatureUI: TNewCheckBox;
  FeatureBrowser, FeatureMultimodal, FeatureExposePeers, FeatureRestrictGroups: TNewCheckBox;
  FeatureA2A: TNewCheckBox;

procedure ApplyProviderDefaults;
var
  Idx: Integer;
begin
  Idx := LLMProviderCombo.ItemIndex;
  if Idx < 0 then Idx := 0;
  case Idx of
    0: begin LLMDetailPage.Values[0] := 'https://api.deepseek.com'; LLMDetailPage.Values[1] := 'deepseek-chat'; end;
    1: begin LLMDetailPage.Values[0] := 'https://api.openai.com/v1'; LLMDetailPage.Values[1] := 'gpt-4o-mini'; end;
    2: begin LLMDetailPage.Values[0] := 'https://dashscope.aliyuncs.com/compatible-mode/v1'; LLMDetailPage.Values[1] := 'qwen-plus'; end;
    3: begin LLMDetailPage.Values[0] := 'http://127.0.0.1:8000/v1'; LLMDetailPage.Values[1] := 'your-model-name'; end;
    4: begin LLMDetailPage.Values[0] := ''; LLMDetailPage.Values[1] := 'mock'; end;
  end;
end;

procedure LLMProviderComboChange(Sender: TObject);
begin
  ApplyProviderDefaults;
end;

function PolicyExistsAt(const AppDir: string): Boolean;
begin
  Result := FileExists(AppDir + '\.runtime\policy\tool.approval.txt');
end;

function ProviderName: string;
begin
  case LLMProviderCombo.ItemIndex of
    1: Result := 'openai';
    2: Result := 'qwen';
    3: Result := 'vllm';
  else
    Result := 'deepseek';
  end;
end;

function UseMockLLM: Boolean;
begin
  Result := (LLMProviderCombo.ItemIndex = 4);
end;

procedure CreateWizardPages;
var
  TopY: Integer;
begin
  { 批次 1/3：LLM }
  LLMProviderPage := CreateCustomPage(wpSelectDir,
    'LLM 配置 (1/3)', '选择大模型 Provider 并填写连接信息。',
    'API Key 请在安装完成后写入系统环境变量（默认变量名 OPENAI_API_KEY）。');
  TopY := 16;
  with TNewStaticText.Create(LLMProviderPage) do
  begin
    Parent := LLMProviderPage.Surface;
    Caption := 'Provider：';
    Left := 0; Top := TopY; Width := LLMProviderPage.SurfaceWidth;
  end;
  TopY := TopY + 20;
  LLMProviderCombo := TNewComboBox.Create(LLMProviderPage);
  with LLMProviderCombo do
  begin
    Parent := LLMProviderPage.Surface;
    Style := csDropDownList;
    Left := 0; Top := TopY; Width := LLMProviderPage.SurfaceWidth;
    Items.Add('DeepSeek（推荐）');
    Items.Add('OpenAI');
    Items.Add('Qwen（通义）');
    Items.Add('vLLM（本地 OpenAI 兼容）');
    Items.Add('Mock（无需 API Key，仅测试）');
    ItemIndex := 0;
    OnChange := @LLMProviderComboChange;
  end;

  LLMDetailPage := CreateInputQueryPage(LLMProviderPage.ID,
    'LLM 连接详情', '填写 Base URL 与模型名。',
    'Mock 模式可留空 Base URL；真实调用须配置 API Key 环境变量。');
  LLMDetailPage.Add('Base URL:', False);
  LLMDetailPage.Add('Model:', False);
  LLMDetailPage.Add('API Key 环境变量名:', False);
  LLMDetailPage.Values[2] := 'OPENAI_API_KEY';
  ApplyProviderDefaults;

  { 批次 2/3：Manage }
  ManageEnablePage := CreateCustomPage(LLMDetailPage.ID,
    'Manage 配置 (2/3)', '是否连接 DAgents Manage 控制台（注册、A2A、Release Hub）。',
    '纯本机助手可跳过；企业内网通常启用 Manage。');
  ManageEnableCheck := TNewCheckBox.Create(ManageEnablePage);
  with ManageEnableCheck do
  begin
    Parent := ManageEnablePage.Surface;
    Caption := '启用 Manage 注册与通信';
    Left := 0; Top := 8; Width := ManageEnablePage.SurfaceWidth;
    Checked := False;
  end;

  ManageDetailPage := CreateInputQueryPage(ManageEnablePage.ID,
    'Manage 连接详情', '填写 Manage 服务地址与注册信息。',
    'registration.base_url 为 Manage 可访问的本 Node 地址；单机可留空。');
  ManageDetailPage.Add('Manage URL:', False);
  ManageDetailPage.Add('Console 分组 (team):', False);
  ManageDetailPage.Add('Registration base_url（可选）:', False);
  ManageDetailPage.Values[0] := 'http://127.0.0.1:8020';
  ManageDetailPage.Values[1] := 'platform';
  ManageDetailPage.Values[2] := '';

  { 批次 3/3：功能开关 }
  FeaturesPage := CreateCustomPage(ManageDetailPage.ID,
    '功能开关 (3/3)', '选择要启用的能力与工具组。',
    '浏览器工具需本机已安装 Chrome；发布包已含 dagents-browser.exe（config 中 browser.enabled: true 时用 dagents browser 启动）。');
  TopY := 0;
  FeatureSkills := TNewCheckBox.Create(FeaturesPage);
  with FeatureSkills do begin Parent := FeaturesPage.Surface; Caption := 'Skills'; Left := 0; Top := TopY; Width := 200; Checked := True; end;
  TopY := TopY + 24;
  FeatureTriggers := TNewCheckBox.Create(FeaturesPage);
  with FeatureTriggers do begin Parent := FeaturesPage.Surface; Caption := 'Triggers（定时任务）'; Left := 0; Top := TopY; Width := 260; Checked := True; end;
  TopY := TopY + 24;
  FeatureChildAgents := TNewCheckBox.Create(FeaturesPage);
  with FeatureChildAgents do begin Parent := FeaturesPage.Surface; Caption := 'Child Agents（子 Agent）'; Left := 0; Top := TopY; Width := 260; Checked := True; end;
  TopY := TopY + 24;
  FeatureUI := TNewCheckBox.Create(FeaturesPage);
  with FeatureUI do begin Parent := FeaturesPage.Surface; Caption := 'Web UI (/ui/)'; Left := 0; Top := TopY; Width := 260; Checked := True; end;
  TopY := TopY + 24;
  FeatureBrowser := TNewCheckBox.Create(FeaturesPage);
  with FeatureBrowser do begin Parent := FeaturesPage.Surface; Caption := 'Browser 工具（browser-use 薄服务）'; Left := 0; Top := TopY; Width := 360; Checked := False; end;
  TopY := TopY + 24;
  FeatureMultimodal := TNewCheckBox.Create(FeaturesPage);
  with FeatureMultimodal do begin Parent := FeaturesPage.Surface; Caption := '多模态 / Vision（read_image + 浏览器视觉模式）'; Left := 0; Top := TopY; Width := 400; Checked := False; end;
  TopY := TopY + 24;
  FeatureExposePeers := TNewCheckBox.Create(FeaturesPage);
  with FeatureExposePeers do begin Parent := FeaturesPage.Surface; Caption := 'expose_to_peers（允许被其它 Agent 调用）'; Left := 0; Top := TopY; Width := 400; Checked := False; end;
  TopY := TopY + 24;
  FeatureA2A := TNewCheckBox.Create(FeaturesPage);
  with FeatureA2A do begin Parent := FeaturesPage.Surface; Caption := 'A2A 工具组（须启用 Manage）'; Left := 0; Top := TopY; Width := 400; Checked := False; end;
  TopY := TopY + 24;
  FeatureRestrictGroups := TNewCheckBox.Create(FeaturesPage);
  with FeatureRestrictGroups do begin Parent := FeaturesPage.Surface; Caption := '显式写入 tools.enabled_groups（否则启用全部工具组）'; Left := 0; Top := TopY; Width := 420; Checked := False; end;
end;

procedure InitializeWizard;
begin
  CreateWizardPages;
  WizardForm.WelcomeLabel1.Caption := '欢迎安装 DAgents 本地助手';
  WizardForm.WelcomeLabel2.Caption :=
    '本安装包包含 Agent Node、Client 与 CLI。' + #13#10 +
    '向导将分三批配置 LLM、Manage 与功能开关，并生成 config.yaml。';
  WizardForm.FinishedLabel.Caption := 'DAgents 已安装完成。';
  WizardForm.FinishedHeadingLabel.Caption := '安装完成';
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := False;
  if (PageID = ManageDetailPage.ID) and (not ManageEnableCheck.Checked) then
    Result := True;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  AppDir: string;
begin
  Result := True;
  if CurPageID = LLMDetailPage.ID then
  begin
    if (not UseMockLLM) and (Trim(LLMDetailPage.Values[1]) = '') then
    begin
      MsgBox('请填写 Model。', mbError, MB_OK);
      Result := False;
      Exit;
    end;
  end;
  if CurPageID = ManageDetailPage.ID then
  begin
    if ManageEnableCheck.Checked and (Trim(ManageDetailPage.Values[0]) = '') then
    begin
      MsgBox('启用 Manage 时须填写 Manage URL。', mbError, MB_OK);
      Result := False;
      Exit;
    end;
    if ManageEnableCheck.Checked then
      FeatureA2A.Checked := True;
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

function JsonEscape(const S: string): string;
var
  I: Integer;
  C: string;
begin
  Result := '';
  for I := 1 to Length(S) do
  begin
    C := S[I];
    if C = '\' then Result := Result + '\\'
    else if C = '"' then Result := Result + '\"'
    else if C = #13 then Result := Result + '\r'
    else if C = #10 then Result := Result + '\n'
    else Result := Result + C;
  end;
end;

function BoolJson(B: Boolean): string;
begin
  if B then Result := 'true' else Result := 'false';
end;

procedure WriteInstallSettingsJson(const JsonPath: string);
var
  Prov: string;
begin
  Prov := ProviderName;
  SaveStringToFile(JsonPath,
    '{' + #13#10 +
    '  "llm": {' + #13#10 +
    '    "provider": "' + JsonEscape(Prov) + '",' + #13#10 +
    '    "base_url": "' + JsonEscape(Trim(LLMDetailPage.Values[0])) + '",' + #13#10 +
    '    "model": "' + JsonEscape(Trim(LLMDetailPage.Values[1])) + '",' + #13#10 +
    '    "mock": ' + BoolJson(UseMockLLM) + ',' + #13#10 +
    '    "api_key_env": "' + JsonEscape(Trim(LLMDetailPage.Values[2])) + '"' + #13#10 +
    '  },' + #13#10 +
    '  "manage": {' + #13#10 +
    '    "enabled": ' + BoolJson(ManageEnableCheck.Checked) + ',' + #13#10 +
    '    "url": "' + JsonEscape(Trim(ManageDetailPage.Values[0])) + '",' + #13#10 +
    '    "team": "' + JsonEscape(Trim(ManageDetailPage.Values[1])) + '",' + #13#10 +
    '    "registration_base_url": "' + JsonEscape(Trim(ManageDetailPage.Values[2])) + '",' + #13#10 +
    '    "a2a_enabled": ' + BoolJson(FeatureA2A.Checked and ManageEnableCheck.Checked) + #13#10 +
    '  },' + #13#10 +
    '  "features": {' + #13#10 +
    '    "expose_to_peers": ' + BoolJson(FeatureExposePeers.Checked) + ',' + #13#10 +
    '    "skills_enabled": ' + BoolJson(FeatureSkills.Checked) + ',' + #13#10 +
    '    "triggers_enabled": ' + BoolJson(FeatureTriggers.Checked) + ',' + #13#10 +
    '    "child_agents_enabled": ' + BoolJson(FeatureChildAgents.Checked) + ',' + #13#10 +
    '    "ui_enabled": ' + BoolJson(FeatureUI.Checked) + ',' + #13#10 +
    '    "browser_enabled": ' + BoolJson(FeatureBrowser.Checked) + ',' + #13#10 +
    '    "multimodal_enabled": ' + BoolJson(FeatureMultimodal.Checked) + ',' + #13#10 +
    '    "restrict_tool_groups": ' + BoolJson(FeatureRestrictGroups.Checked) + #13#10 +
    '  }' + #13#10 +
    '}', False);
end;

function ShouldWriteConfig(const AppDir: string): Boolean;
begin
  if not FileExists(AppDir + '\config.yaml') then
  begin
    Result := True;
    Exit;
  end;
  Result :=
  (MsgBox(
    '已存在 config.yaml。' + #13#10 +
    '是否用本次向导配置覆盖？' + #13#10#13#10 +
    '选「否」将保留现有 config.yaml。',
    mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES);
end;

procedure GenerateConfigYaml;
var
  AppDir, JsonPath, Ps1, TemplatePath, OutPath, CmdLine: string;
  ResultCode: Integer;
begin
  AppDir := ExpandConstant('{app}');
  if not ShouldWriteConfig(AppDir) then
    Exit;
  JsonPath := ExpandConstant('{tmp}\dagents-install-settings.json');
  WriteInstallSettingsJson(JsonPath);
  TemplatePath := AppDir + '\config.example.yaml';
  OutPath := AppDir + '\config.yaml';
  Ps1 := AppDir + '\scripts\windows\write-install-config.ps1';
  if not FileExists(Ps1) then
  begin
    MsgBox('缺少配置脚本: ' + Ps1, mbError, MB_OK);
    Exit;
  end;
  CmdLine := '-NoProfile -ExecutionPolicy Bypass -File "' + Ps1 + '" -TemplatePath "' + TemplatePath + '" -OutputPath "' + OutPath + '" -SettingsPath "' + JsonPath + '"';
  if not Exec('powershell.exe', CmdLine, '', SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) then
    MsgBox('生成 config.yaml 失败（PowerShell 退出码 ' + IntToStr(ResultCode) + '）。' + #13#10 +
      '可手动复制 config.example.yaml 为 config.yaml。', mbError, MB_OK);
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
  if not FeatureBrowser.Checked then
    Exit;
  AppDir := ExpandConstant('{app}');
  ForceDirectories(AppDir + '\.runtime\browser');
  ForceDirectories(AppDir + '\.runtime\browser\profiles');
end;

procedure ShowPostInstallTips;
var
  Tips: string;
begin
  Tips := '';
  if ManageEnableCheck.Checked then
    Tips := Tips + '• Manage 已启用：' + Trim(ManageDetailPage.Values[0]) + #13#10;
  if FeatureBrowser.Checked then
    Tips := Tips + '• Browser 已启用：执行 dagents browser 启动薄服务（默认 127.0.0.1:18766；需本机 Chrome）' + #13#10;
  if UseMockLLM then
    Tips := Tips + '• 当前为 Mock LLM，生产环境请编辑 config.yaml 并设置 API Key' + #13#10;
  if not UseMockLLM then
    Tips := Tips + '• 请设置环境变量 ' + Trim(LLMDetailPage.Values[2]) + #13#10;
  if Tips <> '' then
    MsgBox('后续步骤：' + #13#10 + Tips, mbInformation, MB_OK);
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
    Exec(AppDir + '\dagents.cmd', 'node shutdown', AppDir, SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Path, AppDir, ExternalToolsDir: string;
begin
  if CurStep = ssPostInstall then
  begin
    ApplyPolicySeed;
    GenerateConfigYaml;
    EnsureBrowserRuntimeDir;
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
