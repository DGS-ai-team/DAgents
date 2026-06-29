@echo off
setlocal EnableExtensions EnableDelayedExpansion

rem Use "%~dp0." for pushd: a trailing backslash before the closing quote breaks CMD parsing.
pushd "%~dp0." >nul 2>&1
if errorlevel 1 goto dagents_pushd_failed
set "DAGENTS_HOME=%CD%"

if not exist "config.yaml" if exist "config.example.yaml" copy /Y "config.example.yaml" "config.yaml" >nul
if not exist ".env" if exist ".env.example" copy /Y ".env.example" ".env" >nul

set "CFG=config.yaml"
if defined DAGENTS_CONFIG set "CFG=%DAGENTS_CONFIG%"
set "CFG_ABS=%DAGENTS_HOME%\%CFG%"
set "NODE_PID_FILE=%DAGENTS_HOME%\.runtime\node.pid"

if "%~1"=="" goto cli_default_chat
if /I "%~1"=="help" goto help
if /I "%~1"=="--help" goto help
if /I "%~1"=="-h" goto help
goto dispatch

:cli_default_chat
if not exist "bin\dagents-cli.exe" goto missing_cli
bin\dagents-cli.exe chat --config "%CFG%"
goto cli_exit

:dispatch
if /I "%~1"=="chat" shift & goto run_cli_chat
if /I "%~1"=="tui" shift & goto run_client_tui
if /I "%~1"=="node" shift & goto run_node
if /I "%~1"=="doctor" goto doctor
if /I "%~1"=="version" shift & goto version_cmd
if /I "%~1"=="update" shift & goto update_cmd
goto run_cli_pass

:run_cli_chat
if not exist "bin\dagents-cli.exe" goto missing_cli
set "WITHNODE=0"
set "CLI_EXTRA="
:parse_cli_next
if "%~1"=="" goto run_cli_exec
if /I "%~1"=="--withnode" (
  set "WITHNODE=1"
  shift
  goto parse_cli_next
)
set "CLI_EXTRA=!CLI_EXTRA! %1"
shift
goto parse_cli_next
:run_cli_exec
if "!WITHNODE!"=="1" (
  call :ensure_node
  if errorlevel 1 (
    set "EXIT_CODE=1"
    goto cli_exit
  )
)
bin\dagents-cli.exe chat --config "%CFG%" !CLI_EXTRA!
goto cli_exit

:run_cli_pass
if not exist "bin\dagents-cli.exe" goto missing_cli
bin\dagents-cli.exe %*
goto cli_exit

:run_client_tui
if not exist "bin\dagents-client.exe" goto missing_client
set "WITHNODE=0"
set "TUI_EXTRA="
:parse_tui_next
if "%~1"=="" goto run_tui_exec
if /I "%~1"=="--withnode" (
  set "WITHNODE=1"
  shift
  goto parse_tui_next
)
set "TUI_EXTRA=!TUI_EXTRA! %1"
shift
goto parse_tui_next
:run_tui_exec
if "!WITHNODE!"=="1" (
  call :ensure_node
  if errorlevel 1 (
    set "EXIT_CODE=1"
    goto cli_exit
  )
)
bin\dagents-client.exe -config "%CFG%" tui !TUI_EXTRA!
goto cli_exit

:run_node
if not exist "bin\dagents-node.exe" goto missing_node
if "%~1"=="" (
  call :start_node_default
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="shutdown" (
  shift
  call :shutdown_node
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="stop" (
  shift
  call :shutdown_node
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="restart" (
  shift
  call :restart_node
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="--foreground" (
  shift
  bin\dagents-node.exe -config "%CFG%" %*
  goto cli_exit
)
if /I "%~1"=="-f" (
  shift
  bin\dagents-node.exe -config "%CFG%" %*
  goto cli_exit
)
if /I "%~1"=="--no-wait" goto run_node_nowait
if /I "%~1"=="--background" goto run_node_nowait
bin\dagents-node.exe -config "%CFG%" %*
goto cli_exit

:run_node_nowait
shift
call :probe_node
if not errorlevel 1 (
  echo [dagents] node already running
  call :print_webui_url
  set "EXIT_CODE=0"
  goto cli_exit
)
call :start_node_background
set "EXIT_CODE=!ERRORLEVEL!"
goto cli_exit

:start_node_default
call :probe_node
if not errorlevel 1 (
  echo [dagents] node already running
  call :print_webui_url
  exit /b 0
)
call :start_node_background
if errorlevel 1 exit /b 1
call :wait_node_ready
if errorlevel 1 exit /b 1
echo [dagents] node is ready
call :print_webui_url
exit /b 0

:restart_node
call :shutdown_node
if errorlevel 1 exit /b 1
call :start_node_background
if errorlevel 1 exit /b 1
call :wait_node_ready
if errorlevel 1 exit /b 1
echo [dagents] node restarted
call :print_webui_url
exit /b 0

:print_webui_url
if exist "%DAGENTS_HOME%\scripts\webui-url.bat" (
  <nul set /p="[dagents] Web UI: "
  call "%DAGENTS_HOME%\scripts\webui-url.bat" "%CFG_ABS%"
) else (
  echo [dagents] Web UI: http://127.0.0.1:18765/ui/
)
exit /b 0

:start_node_background
if not exist "%DAGENTS_HOME%\bin\dagents-node.exe" exit /b 1
if not exist "%DAGENTS_HOME%\.runtime\logs" mkdir "%DAGENTS_HOME%\.runtime\logs"
set "NODE_EXE=%DAGENTS_HOME%\bin\dagents-node.exe"
set "NODE_LOG=%DAGENTS_HOME%\.runtime\logs\node.log"
set "NODE_ERR=%DAGENTS_HOME%\.runtime\logs\node.err.log"
set "NODE_START_PS1=%DAGENTS_HOME%\scripts\startup\windows\start-node-detached.ps1"
echo [dagents] starting node in background (logs: %NODE_LOG%)
if not exist "%NODE_START_PS1%" (
  echo [dagents] missing %NODE_START_PS1%
  exit /b 1
)
powershell -NoProfile -ExecutionPolicy Bypass -File "%NODE_START_PS1%" ^
  -NodeExe "%NODE_EXE%" ^
  -Config "%CFG%" ^
  -WorkingDirectory "%DAGENTS_HOME%" ^
  -LogOut "%NODE_LOG%" ^
  -LogErr "%NODE_ERR%" ^
  -PidFile "%NODE_PID_FILE%"
if errorlevel 1 exit /b 1
exit /b 0

:clear_node_pid
if exist "%NODE_PID_FILE%" del /f /q "%NODE_PID_FILE%" >nul 2>&1
exit /b 0

:shutdown_node
call :probe_node
if errorlevel 1 (
  call :clear_node_pid
  echo [dagents] node is not running
  exit /b 0
)
set "NODE_PID="
if exist "%NODE_PID_FILE%" set /p NODE_PID=<"%NODE_PID_FILE%"
if defined NODE_PID (
  call :stop_node_process !NODE_PID!
) else (
  call :find_and_stop_node_pids
)
call :clear_node_pid
call :probe_node
if not errorlevel 1 (
  echo [dagents] node still responds to probe after shutdown; check .runtime\logs\node.err.log
  exit /b 1
)
echo [dagents] node stopped
exit /b 0

:stop_node_process
set "TARGET_PID=%~1"
if not defined TARGET_PID exit /b 0
tasklist /FI "PID eq %TARGET_PID%" 2>nul | find /I "%TARGET_PID%" >nul
if errorlevel 1 exit /b 0
echo [dagents] stopping node (pid=%TARGET_PID%)
taskkill /PID %TARGET_PID% /T >nul 2>&1
set /a STOP_WAIT=0
:stop_node_wait
tasklist /FI "PID eq %TARGET_PID%" 2>nul | find /I "%TARGET_PID%" >nul
if errorlevel 1 exit /b 0
timeout /t 1 /nobreak >nul 2>nul
if errorlevel 1 ping -n 2 127.0.0.1 >nul
set /a STOP_WAIT+=1
if !STOP_WAIT! lss 15 goto stop_node_wait
taskkill /PID %TARGET_PID% /T /F >nul 2>&1
exit /b 0

:find_and_stop_node_pids
powershell -NoProfile -ExecutionPolicy Bypass -Command "$home='!DAGENTS_HOME!'; $cfg='!CFG!'; $cfgAbs='!CFG_ABS!'; Get-CimInstance Win32_Process -Filter 'Name=\"dagents-node.exe\"' | Where-Object { $cl=$_.CommandLine; ($cl -like \"*$cfgAbs*\") -or ($cl -like \"*-config* $cfg*\") -or ($cl -like \"*-config `\"$cfg`\"*\") -or ($cl -like \"*$($home)\bin\dagents-node.exe*\") } | ForEach-Object { Write-Host ('[dagents] stopping node (pid=' + $_.ProcessId + ')'); Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
exit /b 0

:wait_node_ready
set /a NODE_WAIT=0
:wait_node_ready_loop
call :probe_node
if not errorlevel 1 exit /b 0
timeout /t 1 /nobreak >nul 2>nul
if errorlevel 1 ping -n 2 127.0.0.1 >nul
set /a NODE_WAIT+=1
if !NODE_WAIT! lss 30 goto wait_node_ready_loop
call :ensure_node_failed
exit /b 1

:probe_node
if not exist "bin\dagents-client.exe" exit /b 1
bin\dagents-client.exe -config "%CFG_ABS%" probe >nul 2>&1
exit /b %ERRORLEVEL%

:ensure_node
if not exist "bin\dagents-client.exe" (
  echo [dagents] --withnode requires bin\dagents-client.exe ^(probe^)
  exit /b 1
)
call :probe_node
if not errorlevel 1 exit /b 0
call :start_node_background
if errorlevel 1 exit /b 1
call :wait_node_ready
exit /b %ERRORLEVEL%

:ensure_node_failed
echo [dagents] node did not become ready within 30s
echo [dagents] install dir: %DAGENTS_HOME%
echo [dagents] probe detail:
bin\dagents-client.exe -config "%CFG_ABS%" probe
if exist "%DAGENTS_HOME%\.runtime\logs\node.err.log" (
  echo [dagents] -------- node.err.log --------
  type "%DAGENTS_HOME%\.runtime\logs\node.err.log"
  echo [dagents] ---------------------------
)
if exist "%DAGENTS_HOME%\.runtime\logs\node.log" (
  echo [dagents] hint: also check %DAGENTS_HOME%\.runtime\logs\node.log
)
exit /b 1

:missing_cli
echo [dagents] bin\dagents-cli.exe was not found in "%DAGENTS_HOME%"
popd >nul
exit /b 1

:missing_client
echo [dagents] bin\dagents-client.exe was not found in "%DAGENTS_HOME%"
popd >nul
exit /b 1

:missing_node
echo [dagents] bin\dagents-node.exe was not found in "%DAGENTS_HOME%"
popd >nul
exit /b 1

:cli_exit
set "EXIT_CODE=%ERRORLEVEL%"
popd >nul
exit /b %EXIT_CODE%

:doctor
echo DAgents installation: %DAGENTS_HOME%
set "OK=1"
for %%F in (bin\dagents-node.exe bin\dagents-client.exe bin\dagents-cli.exe) do (
  if exist "%%F" (echo [ok] %%F) else (echo [missing] %%F & set "OK=0")
)
if exist "config.yaml" (echo [ok] config.yaml) else (echo [info] config.yaml not found; copy config.example.yaml config.yaml)
if exist ".runtime" (echo [ok] .runtime) else (echo [missing] .runtime)
if "%OK%"=="1" (popd >nul & exit /b 0)
popd >nul
exit /b 1

:version_cmd
if /I "%~1"=="--check" (
  if not exist "bin\dagents-client.exe" goto missing_client
  call :ensure_node
  if errorlevel 1 (
    set "EXIT_CODE=1"
    goto cli_exit
  )
  bin\dagents-client.exe -config "%CFG_ABS%" update --check
  goto cli_exit
)
goto version

:update_cmd
if not exist "bin\dagents-client.exe" goto missing_client
call :ensure_node
if errorlevel 1 (
  set "EXIT_CODE=1"
  goto cli_exit
)
set "UPDATE_FORCE="
set "UPDATE_CHECK="
:parse_update_next
if "%~1"=="" goto run_update_exec
if /I "%~1"=="--force" (
  set "UPDATE_FORCE=--force"
  shift
  goto parse_update_next
)
if /I "%~1"=="--check" (
  set "UPDATE_CHECK=--check"
  shift
  goto parse_update_next
)
if /I "%~1"=="--from-client" (
  shift
  goto parse_update_next
)
echo [dagents] unknown update option: %~1
set "EXIT_CODE=2"
goto cli_exit
:run_update_exec
if defined UPDATE_CHECK (
  bin\dagents-client.exe -config "%CFG_ABS%" update --check
  goto cli_exit
)
set "TMP_PKG=%DAGENTS_HOME%\.runtime\dagents-update-%RANDOM%.pkg"
bin\dagents-client.exe -config "%CFG_ABS%" update --output "%TMP_PKG%" !UPDATE_FORCE!
set "UPDATE_RC=!ERRORLEVEL!"
if "!UPDATE_RC!"=="3" (
  if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
  echo [dagents] already up to date
  goto cli_exit
)
if not "!UPDATE_RC!"=="0" (
  if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
  set "EXIT_CODE=!UPDATE_RC!"
  goto cli_exit
)
call :shutdown_node
if errorlevel 1 (
  if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
  set "EXIT_CODE=1"
  goto cli_exit
)
powershell -NoProfile -Command ^
  "$pkg='%TMP_PKG%'; $prefix='%DAGENTS_HOME%';" ^
  "$staging=Join-Path $env:TEMP ('dagents-update-' + [guid]::NewGuid().ToString());" ^
  "New-Item -ItemType Directory -Path $staging | Out-Null;" ^
  "try { if ($pkg -match '\.zip$') { Expand-Archive -Path $pkg -DestinationPath $staging -Force } else { tar -xf $pkg -C $staging } } catch { exit 2 };" ^
  "$bundle=Get-ChildItem -Path $staging -Directory | Select-Object -First 1;" ^
  "if (-not $bundle) { exit 2 };" ^
  "Copy-Item -Path (Join-Path $bundle.FullName 'bin\*') -Destination (Join-Path $prefix 'bin') -Recurse -Force;" ^
  "if (Test-Path (Join-Path $bundle.FullName 'dagents.cmd')) { Copy-Item -Path (Join-Path $bundle.FullName 'dagents.cmd') -Destination $prefix -Force };" ^
  "if (Test-Path (Join-Path $bundle.FullName 'VERSION')) { Copy-Item -Path (Join-Path $bundle.FullName 'VERSION') -Destination $prefix -Force };" ^
  "Remove-Item -Recurse -Force $staging; exit 0"
set "INSTALL_RC=!ERRORLEVEL!"
if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
if not "!INSTALL_RC!"=="0" (
  echo [dagents] update install failed
  set "EXIT_CODE=!INSTALL_RC!"
  goto cli_exit
)
call :restart_node
echo [dagents] update complete
goto cli_exit

:version
set "APP_VER=unknown"
if exist "VERSION" (
  set /p APP_VER=<VERSION
)
if /I "!APP_VER!"=="unknown" if exist "bin\dagents-cli.exe" (
  for /f "tokens=2" %%V in ('bin\dagents-cli.exe version 2^>nul') do set "APP_VER=%%V"
)
echo DAgents Local Assistant !APP_VER!
if exist "bin\dagents-node.exe" (
  for /f "delims=" %%V in ('bin\dagents-node.exe version 2^>nul') do echo   dagents-node: %%V
)
if exist "bin\dagents-client.exe" (
  for /f "delims=" %%V in ('bin\dagents-client.exe version 2^>nul') do echo   dagents-client: %%V
)
if exist "bin\dagents-cli.exe" (
  for /f "tokens=2" %%V in ('bin\dagents-cli.exe version 2^>nul') do echo   dagents-cli: %%V
)
popd >nul
exit /b 0

:help
echo Usage:
echo   dagents chat [--withnode]       Textual TUI (Python; rich UI)
echo   dagents tui [--withnode] [--plain]  Go bubbletea TUI (default full-screen; --plain for line REPL)
echo   dagents node                    Start Agent Node in background (default; waits until ready)
echo   dagents node shutdown           Stop background Node
echo   dagents node restart            Stop then start Node in background
echo   dagents node --foreground       Start Node in foreground (blocks terminal)
echo   dagents node --no-wait          Background start without waiting for probe
echo   dagents doctor                  Check installed files
echo   dagents version                 Print version information
echo   dagents version --check         Check for updates (via Manage Release Hub)
echo   dagents update [--check]        Download and apply latest release
echo   dagents update --force          Apply without confirmation prompt
echo.
echo Web UI (browser Client, embedded in dagents-node):
echo   After dagents node, open http://127.0.0.1:^<listen.port^>/ui/ (default 18765).
echo   Disable with ui.enabled: false in config.yaml. No separate UI package required.
echo.
echo Background node survives closing this terminal (detached via Start-Process).
echo   For boot persistence use scripts\windows\install_node_service.cmd (admin).
echo.
echo Options:
echo   --withnode     Probe Node first; start it in background if not running, then launch client
echo   --foreground   Run Node in foreground (blocks terminal)
echo   --no-wait      Background start without waiting for probe (--background alias)
echo.
echo Config:
echo   Edit config.yaml (LLM, listen, agent_id). Created from config.example.yaml on first run.
echo   A2A / Registry: deploy Manage separately (packaging/manage/README.md).
echo   CLI override: DAGENTS_CONFIG or DAGENTS_NODE_ENDPOINT
popd >nul
exit /b 0

:dagents_pushd_failed
echo [dagents] Cannot access install directory: "%~dp0."
exit /b 1
