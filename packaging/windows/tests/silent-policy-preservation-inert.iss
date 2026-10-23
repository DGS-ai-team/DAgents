[Setup]
AppId={{8D7B12E6-4E35-4F01-9CB4-1C88F10921A7}}
AppName=DAgents silent policy preservation inert regression
AppVersion=1.0
DefaultDirName={tmp}\dagents-silent-policy-inert-regression
DisableDirPage=no
PrivilegesRequired=lowest
Uninstallable=yes
OutputDir=.
OutputBaseFilename=dagents-silent-policy-inert-regression
Compression=none
SolidCompression=no
CloseApplications=no

[Code]
var
  OverwritePolicy: Boolean;
  OverwritePolicyAnswered: Boolean;

@@POLICY_EXISTS_AT@@
@@NEXT_BUTTON_CLICK@@

procedure CurStepChanged(CurStep: TSetupStep);
var
  AppDir: string;
  ExistingPolicy: TArrayOfString;
begin
  if CurStep <> ssInstall then
    Exit;
  AppDir := ExpandConstant('{app}');
  if not PolicyExistsAt(AppDir) then
    RaiseException('The inert existing policy fixture was not detected.');
  if not OverwritePolicyAnswered then
    RaiseException('Production NextButtonClick did not process the existing policy.');
  if OverwritePolicy then
    RaiseException('Silent default must preserve existing policy.');
  if not LoadStringsFromFile(AppDir + '\.runtime\policy\tool.approval.txt', ExistingPolicy) then
    RaiseException('Existing policy file became unreadable.');
  if (GetArrayLength(ExistingPolicy) <> 1) or (ExistingPolicy[0] <> 'inert-policy-keep') then
    RaiseException('Existing policy content changed.');
  if not SaveStringToFile(AppDir + '\inert-policy-result.txt', 'overwrite=false;policy=preserved', False) then
    RaiseException('Could not record inert policy result.');
end;