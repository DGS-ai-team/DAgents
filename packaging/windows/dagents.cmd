@echo off
setlocal EnableExtensions EnableDelayedExpansion

rem Use "%~dp0." for pushd: a trailing backslash before the closing quote breaks CMD parsing.
pushd "%~dp0." >nul 2>&1
if errorlevel 1 goto dagents_pushd_failed
set "DAGENTS_HOME=%CD%"

if not exist "config.yaml" if exist "config.example.yaml" copy /Y "config.example.yaml" "config.yaml" >nul
if not exist ".env" if exist ".env.example" copy /Y ".env.example" ".env" >nul

set "CFG=config.yaml"
set "CFG_ABS=%DAGENTS_HOME%\%CFG%"

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
if /I "%~1"=="register-center" shift & goto run_register_center
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
if /I "%~1"=="--background" (
  call :start_node_background
  set "EXIT_CODE=!ERRORLEVEL!"
  goto cli_exit
)
bin\dagents-node.exe -config "%CFG%" %*
goto cli_exit

:start_node_background
if not exist "%DAGENTS_HOME%\bin\dagents-node.exe" exit /b 1
if not exist "%DAGENTS_HOME%\.runtime\logs" mkdir "%DAGENTS_HOME%\.runtime\logs"
set "NODE_EXE=%DAGENTS_HOME%\bin\dagents-node.exe"
set "NODE_LOG=%DAGENTS_HOME%\.runtime\logs\node.log"
set "NODE_ERR=%DAGENTS_HOME%\.runtime\logs\node.err.log"
echo [dagents] starting node in background (logs: %NODE_LOG%)
rem /D 固定工作目录；绝对路径避免 start/cmd 子进程 cwd 漂移导致找不到 config 与 .runtime。
start "" /B /D "%DAGENTS_HOME%" cmd /c ""%NODE_EXE%" -config "%CFG_ABS%" 1>>"%NODE_LOG%" 2>>"%NODE_ERR%""
exit /b 0

:ensure_node
if not exist "bin\dagents-client.exe" (
  echo [dagents] --withnode requires bin\dagents-client.exe ^(probe^)
  exit /b 1
)
bin\dagents-client.exe -config "%CFG_ABS%" probe >nul 2>&1
if not errorlevel 1 exit /b 0
call :start_node_background
if errorlevel 1 exit /b 1
timeout /t 2 /nobreak >nul 2>nul
if errorlevel 1 ping -n 3 127.0.0.1 >nul
set /a NODE_WAIT=0
:ensure_node_wait
bin\dagents-client.exe -config "%CFG_ABS%" probe >nul 2>&1
if not errorlevel 1 exit /b 0
timeout /t 1 /nobreak >nul 2>nul
if errorlevel 1 ping -n 2 127.0.0.1 >nul
set /a NODE_WAIT+=1
if !NODE_WAIT! lss 30 goto ensure_node_wait
call :ensure_node_failed
exit /b 1

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

:run_register_center
if not exist "bin\dagents_register_center.exe" goto missing_register_center
bin\dagents_register_center.exe %*
goto cli_exit

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

:missing_register_center
echo [dagents] bin\dagents_register_center.exe was not found in "%DAGENTS_HOME%"
popd >nul
exit /b 1

:cli_exit
set "EXIT_CODE=%ERRORLEVEL%"
popd >nul
exit /b %EXIT_CODE%

:doctor
echo DAgents installation: %DAGENTS_HOME%
set "OK=1"
for %%F in (bin\dagents-node.exe bin\dagents-client.exe bin\dagents-cli.exe bin\dagents_register_center.exe) do (
  if exist "%%F" (echo [ok] %%F) else (echo [missing] %%F & set "OK=0")
)
if exist "config.yaml" (echo [ok] config.yaml) else (echo [info] config.yaml not found; copy config.example.yaml config.yaml)
if exist ".runtime" (echo [ok] .runtime) else (echo [missing] .runtime)
if "%OK%"=="1" (popd >nul & exit /b 0)
popd >nul
exit /b 1

:version
echo DAgents Local Assistant
popd >nul
exit /b 0

:help
echo Usage:
echo   dagents chat [--withnode]   Textual TUI (Python; rich UI)
echo   dagents tui [--withnode] [--plain]  Go bubbletea TUI (default full-screen; --plain for line REPL)
echo   dagents node [--background] Start Agent Node (foreground, or background with logs)
echo   dagents register-center     Start Register Center (optional A2A)
echo   dagents doctor              Check installed files
echo   dagents version             Print version information
echo.
echo Options:
echo   --withnode   Probe Node first; start it in background if not running, then launch client
echo   --background Start Node detached; logs under .runtime\logs\node.log
echo.
echo Config:
echo   Edit config.yaml (LLM, listen, agent_id). Created from config.example.yaml on first run.
echo   Register Center only: copy .env.example to .env for REGISTER_CENTER_* settings.
echo   CLI override: DAGENTS_CONFIG or DAGENTS_NODE_ENDPOINT
popd >nul
exit /b 0

:dagents_pushd_failed
echo [dagents] Cannot access install directory: "%~dp0."
exit /b 1
