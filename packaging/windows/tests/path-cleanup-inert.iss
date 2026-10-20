[Setup]
AppId={{C73B9F58-4D36-44B3-BE63-82DC0D1CA9A4}}
AppName=DAgents PATH cleanup inert regression
AppVersion=1.0
DefaultDirName={tmp}\dagents-path-cleanup-inert-regression
DisableDirPage=yes
PrivilegesRequired=lowest
Uninstallable=yes
OutputDir=.
OutputBaseFilename=dagents-path-cleanup-inert-regression
Compression=none
SolidCompression=no

[Code]
@@REMOVE_PATH_ENTRY@@

function CountSeparators(const Value: string): Integer;
var
  I: Integer;
begin
  Result := 0;
  for I := 1 to Length(Value) do
    if Value[I] = ';' then
      Inc(Result);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  AppDir, SimplePath, SimpleResult, RootResult, LongPath, ExpectedLongPath, Entry: string;
  Output: string;
  I: Integer;
begin
  if CurUninstallStep <> usUninstall then
    Exit;

  { This is a string only; the test never creates or accesses this path. }
  AppDir := 'C:\Users\PathCleanupProbe\DAgents 正式安装验收 Fixture';
  SimplePath := 'A;;' + AppDir + ';B;' + AppDir + '\.runtime\externaltools;' + AppDir + ';C;;';
  SimpleResult := RemovePathEntry(SimplePath, AppDir);
  SimpleResult := RemovePathEntry(SimpleResult, AppDir + '\.runtime\externaltools');
  RootResult := RemovePathEntry('A;C:;C:\;B', 'C:\');

  LongPath := '';
  ExpectedLongPath := '';
  for I := 0 to 16 do
  begin
    Entry := 'C:\Existing\PathSegment' + IntToStr(I);
    if I > 0 then
    begin
      LongPath := LongPath + ';';
      ExpectedLongPath := ExpectedLongPath + ';';
    end;
    LongPath := LongPath + Entry;
    ExpectedLongPath := ExpectedLongPath + Entry;
  end;
  LongPath := LongPath + ';' + AppDir + ';' + AppDir + '\.runtime\externaltools';
  LongPath := RemovePathEntry(LongPath, AppDir);
  LongPath := RemovePathEntry(LongPath, AppDir + '\.runtime\externaltools');

  Output := 'simple_exact=' + IntToStr(Ord(SimpleResult = 'A;;B;C;;')) + #13#10 +
    'long_exact=' + IntToStr(Ord(LongPath = ExpectedLongPath)) + #13#10 +
    'root_exact=' + IntToStr(Ord(RootResult = 'A;C:;B')) + #13#10 +
    'long_token_count=' + IntToStr(CountSeparators(LongPath) + 1) + #13#10;
  if not SaveStringToFile(ExpandConstant('{app}\path-cleanup-result.txt'), Output, False) then
    RaiseException('Unable to write inert test result.');
end;
