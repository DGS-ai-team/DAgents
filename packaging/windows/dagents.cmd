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
set "SHELL_PID_FILE=%DAGENTS_HOME%\.runtime\shell.pid"
set "BROWSER_PID_FILE=%DAGENTS_HOME%\.runtime\browser.pid"

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
if /I "%~1"=="chat" shift & goto tui_removed
if /I "%~1"=="tui" shift & goto tui_removed
if /I "%~1"=="node" shift & goto run_node
if /I "%~1"=="browser" shift & goto run_browser
if /I "%~1"=="shell" shift & goto run_shell
if /I "%~1"=="doctor" goto doctor
if /I "%~1"=="version" shift & goto version_cmd
if /I "%~1"=="update" shift & goto update_cmd
echo [dagents] unknown command: %~1
echo [dagents] run `dagents help` for usage
set "EXIT_CODE=2"
goto cli_exit

:tui_removed
echo [dagents] chat/tui removed in Phase 4. Start the node and open Web UI:
call :print_webui_url
set "EXIT_CODE=1"
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
  call :probe_shell
  if not errorlevel 1 (
    echo [dagents] shell is running
    set "EXIT_CODE=0"
  ) else (
    echo [dagents] shell is not running
    set "EXIT_CODE=1"
  )
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
  goto cli_exit
)
if /I "%~1"=="-f" (
  shift
  bin\dagents-shell.exe -config "%CFG%" %*
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

:start_shell_background
if not exist "%DAGENTS_HOME%\bin\dagents-shell.exe" exit /b 1
if not exist "%DAGENTS_HOME%\.runtime\logs" mkdir "%DAGENTS_HOME%\.runtime\logs"
set "SHELL_EXE=%DAGENTS_HOME%\bin\dagents-shell.exe"
set "SHELL_LOG=%DAGENTS_HOME%\.runtime\logs\shell.log"
set "SHELL_ERR=%DAGENTS_HOME%\.runtime\logs\shell.err.log"
set "SHELL_START_PS1=%DAGENTS_HOME%\scripts\startup\windows\start-shell-detached.ps1"
echo [dagents] starting shell in background (logs: %SHELL_LOG%)
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
call :probe_shell
if errorlevel 1 (
  call :clear_shell_pid
  echo [dagents] shell is not running
  exit /b 0
)
set "SHELL_PID="
if exist "%SHELL_PID_FILE%" set /p SHELL_PID=<"%SHELL_PID_FILE%"
if defined SHELL_PID (
  call :stop_shell_process !SHELL_PID!
) else (
  call :find_and_stop_shell_pids
)
call :clear_shell_pid
call :probe_shell
if not errorlevel 1 (
  echo [dagents] shell still running after shutdown; check .runtime\logs\shell.err.log
  exit /b 1
)
echo [dagents] shell stopped
exit /b 0

:stop_shell_process
set "TARGET_PID=%~1"
if not defined TARGET_PID exit /b 0
tasklist /FI "PID eq %TARGET_PID%" 2>nul | find /I "%TARGET_PID%" >nul
if errorlevel 1 exit /b 0
echo [dagents] stopping shell (pid=%TARGET_PID%)
taskkill /PID %TARGET_PID% /T >nul 2>&1
set /a STOP_WAIT=0
:stop_shell_wait
tasklist /FI "PID eq %TARGET_PID%" 2>nul | find /I "%TARGET_PID%" >nul
if errorlevel 1 exit /b 0
timeout /t 1 /nobreak >nul 2>nul
if errorlevel 1 ping -n 2 127.0.0.1 >nul
set /a STOP_WAIT+=1
if !STOP_WAIT! lss 15 goto stop_shell_wait
taskkill /PID %TARGET_PID% /T /F >nul 2>&1
exit /b 0

:find_and_stop_shell_pids
powershell -NoProfile -ExecutionPolicy Bypass -Command "$home='!DAGENTS_HOME!'; $cfg='!CFG!'; $cfgAbs='!CFG_ABS!'; Get-CimInstance Win32_Process -Filter 'Name=\"dagents-shell.exe\"' | Where-Object { $cl=$_.CommandLine; ($cl -like \"*$cfgAbs*\") -or ($cl -like \"*-config* $cfg*\") -or ($cl -like \"*-config `\"$cfg`\"*\") -or ($cl -like \"*$($home)\bin\dagents-shell.exe*\") } | ForEach-Object { Write-Host ('[dagents] stopping shell (pid=' + $_.ProcessId + ')'); Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
exit /b 0

:probe_shell
set "SHELL_PID="
if exist "%SHELL_PID_FILE%" set /p SHELL_PID=<"%SHELL_PID_FILE%"
if defined SHELL_PID (
  tasklist /FI "PID eq %SHELL_PID%" 2>nul | find /I "%SHELL_PID%" >nul
  if not errorlevel 1 exit /b 0
)
powershell -NoProfile -Command "$home='!DAGENTS_HOME!'; $cfgAbs='!CFG_ABS!'; $cfg='!CFG!'; $found=$false; Get-CimInstance Win32_Process -Filter 'Name=\"dagents-shell.exe\"' | ForEach-Object { $cl=$_.CommandLine; if (($cl -like \"*$cfgAbs*\") -or ($cl -like \"*-config* $cfg*\") -or ($cl -like \"*$($home)\bin\dagents-shell.exe*\")) { $found=$true } }; if ($found) { exit 0 } else { exit 1 }" >nul 2>&1
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
  goto cli_exit
)
if /I "%~1"=="-f" (
  shift
  bin\dagents-browser.exe --config "%CFG%" %*
  goto cli_exit
)
if /I "%~1"=="--no-wait" goto run_browser_nowait
if /I "%~1"=="--background" goto run_browser_nowait
bin\dagents-browser.exe --config "%CFG%" %*
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
set "BROWSER_LOG=%DAGENTS_HOME%\.runtime\logs\browser.log"
set "BROWSER_ERR=%DAGENTS_HOME%\.runtime\logs\browser.err.log"
set "BROWSER_START_PS1=%DAGENTS_HOME%\scripts\startup\windows\start-browser-detached.ps1"
echo [dagents] starting browser service in background (logs: %BROWSER_LOG%)
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
  echo [dagents] browser service still responds after shutdown; check .runtime\logs\browser.err.log
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
powershell -NoProfile -ExecutionPolicy Bypass -Command "$home='!DAGENTS_HOME!'; $cfg='!CFG!'; $cfgAbs='!CFG_ABS!'; Get-CimInstance Win32_Process -Filter 'Name=\"dagents-browser.exe\"' | Where-Object { $cl=$_.CommandLine; ($cl -like \"*$cfgAbs*\") -or ($cl -like \"*--config* $cfg*\") -or ($cl -like \"*--config `\"$cfg`\"*\") -or ($cl -like \"*$($home)\bin\dagents-browser.exe*\") } | ForEach-Object { Write-Host ('[dagents] stopping browser service (pid=' + $_.ProcessId + ')'); Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
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
if exist "%DAGENTS_HOME%\.runtime\logs\browser.err.log" (
  echo [dagents] -------- browser.err.log --------
  type "%DAGENTS_HOME%\.runtime\logs\browser.err.log"
  echo [dagents] ---------------------------
)
exit /b 1

:probe_browser
powershell -NoProfile -Command "try { (Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 http://127.0.0.1:18766/health).StatusCode | Out-Null; exit 0 } catch { exit 1 }" >nul 2>&1
exit /b %ERRORLEVEL%

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
echo [dagents] dagents-cli was removed in Phase 4; use Web UI instead
call :print_webui_url
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
set "EXIT_CODE=%ERRORLEVEL%"
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
  goto cli_exit
)
goto version

:update_cmd
if not exist "bin\dagents-client.exe" goto missing_client
call :probe_shell
if not errorlevel 1 (
  bin\dagents-shell.exe -config "%CFG%" update %*
  set "UPDATE_RC=!ERRORLEVEL!"
  if "!UPDATE_RC!"=="2" goto update_client_fallback
  set "EXIT_CODE=!UPDATE_RC!"
  goto cli_exit
)
:update_client_fallback
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
echo   dagents node shutdown           Stop background Node
echo   dagents node restart            Stop then start Node in background
echo   dagents node --foreground       Start Node in foreground (blocks terminal)
echo   dagents node --no-wait          Background start without waiting for probe
echo   dagents browser                 Start browser-use service in background (browser.enabled)
echo   dagents browser stop            Stop browser-use service
echo   dagents browser --foreground    Run browser service in foreground
echo   dagents shell                   Start Desktop Shell in background (tray + Node)
echo   dagents shell status            Check Shell process
echo   dagents shell stop              Stop Desktop Shell (and Node via tray exit)
echo   dagents shell --foreground      Run Shell in foreground (blocks terminal)
echo   dagents doctor                  Check installed files
echo   dagents version                 Print version information
echo   dagents version --check         Check for updates (via Manage Release Hub)
echo   dagents update [--check]        Download and apply latest release
echo   dagents update --force          Apply without confirmation prompt
echo.
echo Web UI (browser client, embedded in dagents-node):
echo   After `dagents` or `dagents node`, open http://127.0.0.1:^<listen.port^>/ui/ (default 18765).
echo   chat/tui terminal clients were removed in Phase 4; use Web UI instead.
echo   Disable with ui.enabled: false in config.yaml. No separate UI package required.
echo.
echo Background node survives closing this terminal (detached via Start-Process).
echo   Desktop Shell (dagents shell) supervises Node and shows HITL toasts in the tray.
echo   For boot persistence Shell registers login autostart via the Windows installer.
echo   Legacy: scripts\windows\install_node_service.cmd (admin) for Node-only service.
echo.
echo Options:
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
