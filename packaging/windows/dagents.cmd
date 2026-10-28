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
for %%I in ("%CFG%") do set "CFG_ABS=%%~fI"
set "SHELL_PID_FILE=%DAGENTS_HOME%\.runtime\shell.pid"
set "BROWSER_PID_FILE=%DAGENTS_HOME%\.runtime\browser.pid"
call :resolve_log_day

if "%~1"=="" goto default_start_node
if /I "%~1"=="help" goto help
if /I "%~1"=="--help" goto help
if /I "%~1"=="-h" goto help
goto dispatch

:default_start_node
call :start_node_default
set "EXIT_CODE=!ERRORLEVEL!"
goto cli_exit

:dispatch
if /I "%~1"=="chat" (
  shift
  goto tui_removed
)
if /I "%~1"=="tui" (
  shift
  goto tui_removed
)
if /I "%~1"=="node" (
  shift
  goto run_node
)
if /I "%~1"=="browser" (
  shift
  goto run_browser
)
if /I "%~1"=="shell" (
  shift
  goto run_shell
)
if /I "%~1"=="doctor" goto doctor
if /I "%~1"=="version" (
  shift
  goto version_cmd
)
if /I "%~1"=="update" (
  shift
  goto update_cmd
)
echo [dagents] unknown command: %~1
echo [dagents] run `dagents help` for usage
set "EXIT_CODE=2"
goto cli_exit
:tui_removed
echo [dagents] chat/tui removed in Phase 4. Start the node and open Node API:
call :print_node_endpoint
set "EXIT_CODE=1"
goto cli_exit

:run_node
if not exist "bin\dagents-node.exe" goto missing_node
if "%~1"=="" (
  call :start_node_default
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)

if /I "%~1"=="status" (
  shift
  call :status_node
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
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="-f" (
  shift
  bin\dagents-node.exe -config "%CFG%" %*
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="--no-wait" goto run_node_nowait
if /I "%~1"=="--background" goto run_node_nowait
bin\dagents-node.exe -config "%CFG%" %*
set "EXIT_CODE=!ERRORLEVEL!"
goto cli_exit

:run_shell
if not exist "bin\dagents-shell.exe" goto missing_shell
if "%~1"=="" (
  call :start_shell_default
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="start" (
  shift
  call :start_shell_default
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="status" (
  shift
  call :status_shell
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="stop" (
  shift
  call :shutdown_shell
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)

if /I "%~1"=="shutdown" (
  shift
  call :shutdown_shell
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="--foreground" (
  shift
  bin\dagents-shell.exe -config "%CFG%" %*
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="-f" (
  shift
  bin\dagents-shell.exe -config "%CFG%" %*
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="--no-wait" goto run_shell_nowait
if /I "%~1"=="--background" goto run_shell_nowait
call :start_shell_default
set "EXIT_CODE=!ERRORLEVEL!"
goto cli_exit

:run_shell_nowait
shift
call :probe_shell
if not errorlevel 1 (
  echo [dagents] shell already running
  set "EXIT_CODE=0"
  goto cli_exit
)
call :start_shell_background
set "EXIT_CODE=!ERRORLEVEL!"
goto cli_exit

:start_shell_default
call :probe_shell
if not errorlevel 1 (
  echo [dagents] shell already running
  exit /b 0
)
call :start_shell_background
if errorlevel 1 exit /b 1
echo [dagents] shell started (system tray)
exit /b 0

:resolve_log_day
set "LOG_DAY="
for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "Get-Date -Format yyyy-MM-dd"`) do set "LOG_DAY=%%i"
if not defined LOG_DAY set "LOG_DAY=unknown"
exit /b 0

:start_shell_background
if not exist "%DAGENTS_HOME%\bin\dagents-shell.exe" exit /b 1
if not exist "%DAGENTS_HOME%\.runtime\logs" mkdir "%DAGENTS_HOME%\.runtime\logs"
set "SHELL_EXE=%DAGENTS_HOME%\bin\dagents-shell.exe"
set "SHELL_LOG=%DAGENTS_HOME%\.runtime\logs\shell-%LOG_DAY%.log"
set "SHELL_ERR=%DAGENTS_HOME%\.runtime\logs\shell-%LOG_DAY%.err.log"
set "SHELL_START_PS1=%DAGENTS_HOME%\scripts\startup\windows\start-shell-detached.ps1"
echo [dagents] starting shell in background (full: %SHELL_LOG%; errors: %SHELL_ERR%)
if not exist "%SHELL_START_PS1%" (
  echo [dagents] missing %SHELL_START_PS1%
  exit /b 1
)
powershell -NoProfile -ExecutionPolicy Bypass -File "%SHELL_START_PS1%" ^
  -ShellExe "%SHELL_EXE%" ^
  -Config "%CFG%" ^
  -WorkingDirectory "%DAGENTS_HOME%" ^
  -LogOut "%SHELL_LOG%" ^
  -LogErr "%SHELL_ERR%" ^
  -PidFile "%SHELL_PID_FILE%"
if errorlevel 1 exit /b 1
exit /b 0

:clear_shell_pid
if exist "%SHELL_PID_FILE%" del /f /q "%SHELL_PID_FILE%" >nul 2>&1
exit /b 0

:shutdown_shell
echo [dagents] asking this install's Desktop to quit
bin\dagents-shell.exe --quit-desktop --config "%CFG_ABS%"
exit /b %ERRORLEVEL%

:status_shell
call :probe_shell
set "SHELL_STATUS_RC=!ERRORLEVEL!"
bin\dagents-shell.exe --desktop-status --config "%CFG_ABS%"
if not "!SHELL_STATUS_RC!"=="0" exit /b !SHELL_STATUS_RC!
exit /b 0

:probe_shell
bin\dagents-shell.exe --desktop-status --config "%CFG_ABS%" | powershell -NoProfile -Command "$s=[Console]::In.ReadToEnd(); try { $j=ConvertFrom-Json $s; if ($j.running) { exit 0 } else { exit 1 } } catch { exit 2 }"
exit /b %ERRORLEVEL%

:run_browser
if not exist "bin\dagents-browser.exe" goto missing_browser
if "%~1"=="" (
  call :start_browser_default
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)

if /I "%~1"=="shutdown" (
  shift
  call :shutdown_browser
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="stop" (
  shift
  call :shutdown_browser
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="--foreground" (
  shift
  bin\dagents-browser.exe --config "%CFG%" %*
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="-f" (
  shift
  bin\dagents-browser.exe --config "%CFG%" %*
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
if /I "%~1"=="--no-wait" goto run_browser_nowait
if /I "%~1"=="--background" goto run_browser_nowait
bin\dagents-browser.exe --config "%CFG%" %*
set "EXIT_CODE=!ERRORLEVEL!"
goto cli_exit

:run_browser_nowait
shift
call :probe_browser
if not errorlevel 1 (
  echo [dagents] browser service already running
  set "EXIT_CODE=0"
  goto cli_exit
)
call :start_browser_background
set "EXIT_CODE=!ERRORLEVEL!"
goto cli_exit

:start_browser_default
call :probe_browser
if not errorlevel 1 (
  echo [dagents] browser service already running
  exit /b 0
)
call :start_browser_background
if errorlevel 1 exit /b 1
call :wait_browser_ready
if errorlevel 1 exit /b 1
echo [dagents] browser service is ready
exit /b 0

:start_browser_background
if not exist "%DAGENTS_HOME%\bin\dagents-browser.exe" exit /b 1
if not exist "%DAGENTS_HOME%\.runtime\logs" mkdir "%DAGENTS_HOME%\.runtime\logs"
set "BROWSER_EXE=%DAGENTS_HOME%\bin\dagents-browser.exe"
set "BROWSER_LOG=%DAGENTS_HOME%\.runtime\logs\browser-%LOG_DAY%.log"
set "BROWSER_ERR=%DAGENTS_HOME%\.runtime\logs\browser-%LOG_DAY%.err.log"
set "BROWSER_START_PS1=%DAGENTS_HOME%\scripts\startup\windows\start-browser-detached.ps1"
echo [dagents] starting browser service in background (full: %BROWSER_LOG%; errors: %BROWSER_ERR%)
if not exist "%BROWSER_START_PS1%" (
  echo [dagents] missing %BROWSER_START_PS1%
  exit /b 1
)
powershell -NoProfile -ExecutionPolicy Bypass -File "%BROWSER_START_PS1%" ^
  -BrowserExe "%BROWSER_EXE%" ^
  -Config "%CFG%" ^
  -WorkingDirectory "%DAGENTS_HOME%" ^
  -LogOut "%BROWSER_LOG%" ^
  -LogErr "%BROWSER_ERR%" ^
  -PidFile "%BROWSER_PID_FILE%"
if errorlevel 1 exit /b 1
exit /b 0

:clear_browser_pid
if exist "%BROWSER_PID_FILE%" del /f /q "%BROWSER_PID_FILE%" >nul 2>&1
exit /b 0

:shutdown_browser
call :probe_browser
if errorlevel 1 (
  call :clear_browser_pid
  echo [dagents] browser service is not running
  exit /b 0
)
set "BROWSER_PID="
if exist "%BROWSER_PID_FILE%" set /p BROWSER_PID=<"%BROWSER_PID_FILE%"
if defined BROWSER_PID (
  call :stop_browser_process !BROWSER_PID!
) else (
  call :find_and_stop_browser_pids
)
call :clear_browser_pid
call :probe_browser
if not errorlevel 1 (
  echo [dagents] browser service still responds after shutdown; check .runtime\logs\browser-%LOG_DAY%.err.log
  exit /b 1
)
echo [dagents] browser service stopped
exit /b 0

:stop_browser_process
set "TARGET_PID=%~1"
if not defined TARGET_PID exit /b 0
tasklist /FI "PID eq %TARGET_PID%" 2>nul | find /I "%TARGET_PID%" >nul
if errorlevel 1 exit /b 0
echo [dagents] stopping browser service (pid=%TARGET_PID%)
taskkill /PID %TARGET_PID% /T >nul 2>&1
set /a STOP_WAIT=0
:stop_browser_wait
tasklist /FI "PID eq %TARGET_PID%" 2>nul | find /I "%TARGET_PID%" >nul
if errorlevel 1 exit /b 0
timeout /t 1 /nobreak >nul 2>nul
if errorlevel 1 ping -n 2 127.0.0.1 >nul
set /a STOP_WAIT+=1
if !STOP_WAIT! lss 15 goto stop_browser_wait
taskkill /PID %TARGET_PID% /T /F >nul 2>&1
exit /b 0

:find_and_stop_browser_pids
powershell -NoProfile -ExecutionPolicy Bypass -Command "$installRoot='!DAGENTS_HOME!'; $cfg='!CFG!'; $cfgAbs='!CFG_ABS!'; Get-CimInstance Win32_Process -Filter 'Name=\"dagents-browser.exe\"' | Where-Object { $cl=$_.CommandLine; ($cl -like \"*$cfgAbs*\") -or ($cl -like \"*--config* $cfg*\") -or ($cl -like \"*--config `\"$cfg`\"*\") -or ($cl -like \"*$($installRoot)\bin\dagents-browser.exe*\") } | ForEach-Object { Write-Host ('[dagents] stopping browser service (pid=' + $_.ProcessId + ')'); Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
exit /b 0

:wait_browser_ready
set /a BROWSER_WAIT=0
:wait_browser_ready_loop
call :probe_browser
if not errorlevel 1 exit /b 0
timeout /t 1 /nobreak >nul 2>nul
if errorlevel 1 ping -n 2 127.0.0.1 >nul
set /a BROWSER_WAIT+=1
if !BROWSER_WAIT! lss 30 goto wait_browser_ready_loop
echo [dagents] browser service did not become ready within 30s
if exist "%DAGENTS_HOME%\.runtime\logs\browser-%LOG_DAY%.err.log" (
  echo [dagents] -------- browser-%LOG_DAY%.err.log --------
  type "%DAGENTS_HOME%\.runtime\logs\browser-%LOG_DAY%.err.log"
  echo [dagents] ---------------------------
)
exit /b 1

:probe_browser
powershell -NoProfile -Command "try { (Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 http://127.0.0.1:18766/health).StatusCode | Out-Null; exit 0 } catch { exit 1 }" >nul 2>&1
exit /b %ERRORLEVEL%

:run_node_nowait
shift
call :start_node_background
set "EXIT_CODE=!ERRORLEVEL!"
goto cli_exit

:start_node_default
call :start_node_background
set "START_RC=!ERRORLEVEL!"
if not "!START_RC!"=="0" exit /b !START_RC!
call :wait_node_ready
if errorlevel 1 exit /b 1
echo [dagents] node is ready
call :print_node_endpoint
exit /b 0

:restart_node
call :shutdown_node
if errorlevel 1 exit /b 1
call :start_node_background
set "START_RC=!ERRORLEVEL!"
if not "!START_RC!"=="0" exit /b !START_RC!
call :wait_node_ready
if errorlevel 1 exit /b 1
echo [dagents] node restarted
call :print_node_endpoint
exit /b 0

:status_node
if not exist "bin\dagents-shell.exe" goto missing_shell
bin\dagents-shell.exe --node-status --config "%CFG_ABS%"
exit /b %ERRORLEVEL%

:print_node_endpoint
if exist "%DAGENTS_HOME%\scripts\node-api-url.bat" (
  <nul set /p="[dagents] Node API: "
  call "%DAGENTS_HOME%\scripts\node-api-url.bat" "%CFG_ABS%"
) else (
  echo [dagents] Node API: http://127.0.0.1:18765
)
exit /b 0

:start_node_background
if not exist "%DAGENTS_HOME%\bin\dagents-shell.exe" exit /b 1
echo [dagents] starting Desktop-owned Node in background
bin\dagents-shell.exe --start-owned-node --config "%CFG_ABS%"
set "START_RC=!ERRORLEVEL!"
exit /b !START_RC!
:shutdown_node
if not exist "bin\dagents-shell.exe" goto missing_shell
set "NODE_STOP_ARGS=--stop-owned-node"
if /I "%~1"=="--require-idle" set "NODE_STOP_ARGS=--stop-owned-node --require-idle"
bin\dagents-shell.exe !NODE_STOP_ARGS! --config "%CFG_ABS%"
if errorlevel 1 exit /b 1
call :probe_node
if not errorlevel 1 (
  echo [dagents] Node still responds after the owned-process stop; it may be external or not owned by this Desktop.
  exit /b 1
)
echo [dagents] node stopped
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
bin\dagents-shell.exe --node-status --config "%CFG_ABS%" >nul 2>&1
exit /b %ERRORLEVEL%

:ensure_node

call :probe_node
if not errorlevel 1 exit /b 0
call :start_node_background
set "START_RC=!ERRORLEVEL!"
if not "!START_RC!"=="0" exit /b !START_RC!
call :wait_node_ready
exit /b %ERRORLEVEL%

:ensure_node_failed
echo [dagents] node did not become ready within 30s
echo [dagents] install dir: %DAGENTS_HOME%
echo [dagents] probe detail:
bin\dagents-shell.exe --node-status --config "%CFG_ABS%"
if exist "%DAGENTS_HOME%\.runtime\logs\node-%LOG_DAY%.err.log" (
  echo [dagents] -------- node-%LOG_DAY%.err.log --------
  type "%DAGENTS_HOME%\.runtime\logs\node-%LOG_DAY%.err.log"
  echo [dagents] ---------------------------
)
if exist "%DAGENTS_HOME%\.runtime\logs\node-%LOG_DAY%.log" (
  echo [dagents] hint: also check %DAGENTS_HOME%\.runtime\logs\node-%LOG_DAY%.log
)
exit /b 1

:missing_cli
echo [dagents] dagents-cli was removed; use DAgents Desktop to connect to the Node API
call :print_node_endpoint
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

:missing_browser
echo [dagents] bin\dagents-browser.exe was not found in "%DAGENTS_HOME%"
popd >nul
exit /b 1

:missing_shell
echo [dagents] bin\dagents-shell.exe was not found in "%DAGENTS_HOME%"
popd >nul
exit /b 1

:cli_exit
if not defined EXIT_CODE set "EXIT_CODE=%ERRORLEVEL%"
popd >nul
exit /b %EXIT_CODE%

:doctor
echo DAgents installation: %DAGENTS_HOME%
set "OK=1"
for %%F in (bin\dagents-node.exe bin\dagents-client.exe bin\dagents-browser.exe bin\dagents-shell.exe) do (
  if exist "%%F" (echo [ok] %%F) else (
    if "%%F"=="bin\dagents-browser.exe" (echo [optional] %%F) else if "%%F"=="bin\dagents-shell.exe" (echo [optional] %%F) else (echo [missing] %%F & set "OK=0")
  )
)
if exist "bin\dagents-cli.exe" (echo [optional] bin\dagents-cli.exe ^(removed in Phase 4^)) else (echo [info] bin\dagents-cli.exe not present ^(removed in Phase 4^))
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
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
goto version

:update_cmd
call :probe_shell
if not errorlevel 1 (
  bin\dagents-shell.exe -config "%CFG%" update %*
  set "UPDATE_RC=!ERRORLEVEL!"
  if "!UPDATE_RC!"=="2" goto update_client_fallback
  set "EXIT_CODE=!UPDATE_RC!"
  goto cli_exit
)
:update_client_fallback
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
echo [dagents] unknown update option: %~1
set "EXIT_CODE=2"
goto cli_exit
:run_update_exec
if defined UPDATE_CHECK (
  bin\dagents-client.exe -config "%CFG_ABS%" update --check
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
set "TMP_PKG=%DAGENTS_HOME%\.runtime\dagents-update-%RANDOM%.pkg"
bin\dagents-client.exe -config "%CFG_ABS%" update --output "%TMP_PKG%" !UPDATE_FORCE!
set "UPDATE_RC=!ERRORLEVEL!"
if "!UPDATE_RC!"=="3" (
  if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
  echo [dagents] already up to date
  set "EXIT_CODE=0"
  goto cli_exit
)
if not "!UPDATE_RC!"=="0" (
  if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
  set "EXIT_CODE=!UPDATE_RC!"
  goto cli_exit
)
call :shutdown_shell
if errorlevel 1 (
  if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
  set "EXIT_CODE=1"
  goto cli_exit
)
call :shutdown_node --require-idle
if errorlevel 1 (
  if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
  set "EXIT_CODE=1"
  goto cli_exit
)
powershell -NoProfile -Command ^
  "$pkg='%TMP_PKG%';" ^
  "$magic=$null; try { $magic=[System.IO.File]::ReadAllBytes($pkg) } catch { exit 2 };" ^
  "if ($magic.Length -ge 2 -and $magic[0] -eq 0x4D -and $magic[1] -eq 0x5A) { $installer=Join-Path $env:TEMP ('dagents-update-' + [guid]::NewGuid().ToString() + '.exe'); Copy-Item -LiteralPath $pkg -Destination $installer -Force; Start-Process -FilePath $installer -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART'; exit 10 } else { exit 2 }"
set "INSTALL_RC=!ERRORLEVEL!"
if exist "!TMP_PKG!" del /f /q "!TMP_PKG!"
if "!INSTALL_RC!"=="10" (
  echo [dagents] Windows installer started; the new Shell will restart after setup completes
  set "EXIT_CODE=0"
  goto cli_exit
)
if not "!INSTALL_RC!"=="0" (
  echo [dagents] update install failed
  set "EXIT_CODE=!INSTALL_RC!"
  goto cli_exit
)
call :restart_node
set "RESTART_RC=!ERRORLEVEL!"
if not "!RESTART_RC!"=="0" (
  set "EXIT_CODE=!RESTART_RC!"
  goto cli_exit
)
echo [dagents] update complete
set "EXIT_CODE=0"
goto cli_exit

:version
set "APP_VER=unknown"
if exist "VERSION" (
  set /p APP_VER=<VERSION
)
echo DAgents Local Assistant !APP_VER!
if exist "bin\dagents-node.exe" (
  for /f "delims=" %%V in ('bin\dagents-node.exe version 2^>nul') do echo   dagents-node: %%V
)
if exist "bin\dagents-client.exe" (
  for /f "delims=" %%V in ('bin\dagents-client.exe version 2^>nul') do echo   dagents-client: %%V
)
popd >nul
exit /b 0

:help
echo Usage:
echo   dagents                         Start Agent Node in background (default; waits until ready)
echo   dagents node                    Start Agent Node in background (default; waits until ready)
echo   dagents node status             Print Desktop ownership and Node readiness
echo   dagents node shutdown           Stop this Desktop's owned Node only
echo   dagents node restart            Stop owned Node then start Node in background
echo   dagents node --foreground       Start external Node in foreground (Ctrl+C to stop)
echo   dagents node --no-wait          Background start (owned startup verifies health)
echo   dagents browser                 Start Playwright browser service in background (browser.enabled)
echo   dagents browser stop            Stop Playwright browser service
echo   dagents browser --foreground    Run browser service in foreground
echo   dagents shell                   Start DAgents Desktop in the system tray
echo   dagents shell status            Print this Desktop's running status
echo   dagents shell stop              Quit DAgents Desktop; the owned Node keeps running
echo   dagents shell --foreground      Run DAgents Desktop in foreground (blocks terminal)
echo   dagents doctor                  Check installed files
echo   dagents version                 Print version information
echo   dagents version --check         Check for updates (via Manage Release Hub)
echo   dagents update [--check]        Download and apply latest release
echo   dagents update --force          Apply without confirmation prompt
echo.
echo Node API:
echo   Open DAgents Desktop to connect to the local Node API (default http://127.0.0.1:18765).
echo   DAgents Desktop provides the local interface; the Node exposes API endpoints only.
echo.
echo Background Node is owned and supervised by DAgents Desktop; closing this terminal leaves it running.
echo   A foreground Node is external to Desktop ownership; stop it with Ctrl+C in its terminal.
echo   Desktop Shell (dagents shell) shows HITL toasts in the tray.
echo   Optional login startup is selected by the Windows installer task.
echo   Legacy: scripts\windows\install_node_service.cmd (admin) for Node-only service.
echo.
echo Options:
echo   --foreground   Run an external Node in foreground (blocks terminal; stop with Ctrl+C)
echo   --no-wait      Background start; owned startup verifies health (--background alias)
echo.
echo Config:
echo   config.yaml is Node bootstrap-only (listen); configure Node settings in DAgents Desktop.
echo   It is created from config.example.yaml on first run.
echo   A2A / Registry: deploy Manage separately (packaging/manage/README.md).
echo   CLI configuration: local endpoint must use loopback; set via DAGENTS_CONFIG
popd >nul
exit /b 0

:dagents_pushd_failed
echo [dagents] Cannot access install directory: "%~dp0."
exit /b 1
