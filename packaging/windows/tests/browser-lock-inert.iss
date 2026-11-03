[Setup]
AppId=DAgentsBrowserLockInert
AppName=DAgents browser lock inert regression
AppVersion=1.0
DefaultDirName={tmp}\dagents-browser-lock-inert
PrivilegesRequired=lowest
Uninstallable=no
CreateAppDir=yes
UsePreviousAppDir=no
OutputBaseFilename=dagents-browser-lock-inert
Compression=none
CloseApplications=@@CLOSE_APPLICATIONS@@
CloseApplicationsFilter=@@CLOSE_FILTER@@
RestartApplications=@@RESTART_APPLICATIONS@@

[Files]
Source: "@@REPLACEMENT@@"; DestDir: "{app}\bin"; DestName: "dagents-browser.exe"; Flags: ignoreversion
