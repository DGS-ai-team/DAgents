@echo off
setlocal EnableExtensions
rem Dev launcher: MSVC env + cargo/npm PATH, then tauri (avoids PowerShell (x86) / unquoted PATH breakage).
call "%ProgramFiles(x86)%\Microsoft Visual Studio\2022\BuildTools\Common7\Tools\VsDevCmd.bat" -arch=x64
if errorlevel 1 (
  echo dagents-shell: VsDevCmd.bat failed
  exit /b 1
)
set "PATH=%USERPROFILE%\.cargo\bin;%CD%\node_modules\.bin;%PATH%"
if not exist "node_modules\@tauri-apps\cli" (
  call npm install
  if errorlevel 1 exit /b 1
)
rem Tauri 2: first `--` = runner args, second `--` = app args (e.g. --config).
npx --yes tauri dev -- -- %*
exit /b %ERRORLEVEL%
