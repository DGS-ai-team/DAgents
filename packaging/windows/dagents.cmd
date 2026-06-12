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
if /I "%~1"=="version" goto version
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
  exit /b 0
)
call :start_node_background
if errorlevel 1 exit /b 1
call :wait_node_ready
if errorlevel 1 exit /b 1
echo [dagents] node is ready
exit /b 0

:restart_node
call :shutdown_node
if errorlevel 1 exit /b 1
call :start_node_background
if errorlevel 1 exit /b 1
call :wait_node_ready
if errorlevel 1 exit /b 1
echo [dagents] node restarted
exit /b 0

:start_node_background
if not exist "%DAGENTS_HOME%\bin\dagents-node.exe" exit /b 1
if not exist "%DAGENTS_HOME%\.runtime\logs" mkdir "%DAGENTS_HOME%\.runtime\logs"
set "NODE_EXE=%DAGENTS_HOME%\bin\dagents-node.exe"
set "NODE_LOG=%DAGENTS_HOME%\.runtime\logs\node.log"
set "NODE_ERR=%DAGENTS_HOME%\.runtime\logs\node.err.log"
echo [dagents] starting node in background (logs: %NODE_LOG%)
rem /D 固定工作目录；绝对 config 避免 cwd 漂移导致 fs_root 读错目录。
start "" /B /D "%DAGENTS_HOME%" cmd /c ""%NODE_EXE%" -config "%CFG_ABS%" 1>>"%NODE_LOG%" 2>>"%NODE_ERR%""
call :capture_node_pid
exit /b 0

:capture_node_pid
if not exist "%DAGENTS_HOME%\.runtime" mkdir "%DAGENTS_HOME%\.runtime"
timeout /t 1 /nobreak >nul 2>nul
if errorlevel 1 ping -n 2 127.0.0.1 >nul
powershell -NoProfile -ExecutionPolicy Bypass -Command "$cfg='!CFG_ABS!'; $p=Get-CimInstance Win32_Process -Filter 'Name=\"dagents-node.exe\"' | Where-Object { $_.CommandLine -like \"*$cfg*\" } | Select-Object -First 1 -ExpandProperty ProcessId; if ($p) { Set-Content -LiteralPath '!NODE_PID_FILE!' -Value $p -NoNewline }"
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
powershell -NoProfile -ExecutionPolicy Bypass -Command "$cfg='!CFG_ABS!'; Get-CimInstance Win32_Process -Filter 'Name=\"dagents-node.exe\"' | Where-Object { $_.CommandLine -like \"*$cfg*\" } | ForEach-Object { Write-Host ('[dagents] stopping node (pid=' + $_.ProcessId + ')'); Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
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
