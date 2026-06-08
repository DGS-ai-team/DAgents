@echo off
setlocal EnableExtensions

rem Use "%~dp0." for pushd: a trailing backslash before the closing quote breaks CMD parsing.
pushd "%~dp0." >nul 2>&1
if errorlevel 1 goto dagents_pushd_failed
set "DAGENTS_HOME=%CD%"

if not exist "config.yaml" if exist "config.example.yaml" copy /Y "config.example.yaml" "config.yaml" >nul
if not exist ".env" if exist ".env.example" copy /Y ".env.example" ".env" >nul

set "CFG=config.yaml"

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
if /I "%~1"=="chat" shift & goto run_cli
if /I "%~1"=="tui" shift & goto run_client_tui
if /I "%~1"=="node" shift & goto run_node
if /I "%~1"=="register-center" shift & goto run_register_center
if /I "%~1"=="doctor" goto doctor
if /I "%~1"=="version" goto version
goto run_cli

:run_cli
if not exist "bin\dagents-cli.exe" goto missing_cli
bin\dagents-cli.exe %*
goto cli_exit

:run_client_tui
if not exist "bin\dagents-client.exe" goto missing_client
bin\dagents-client.exe -config "%CFG%" tui %*
goto cli_exit

:run_node
if not exist "bin\dagents-node.exe" goto missing_node
bin\dagents-node.exe -config "%CFG%" %*
goto cli_exit

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
echo   dagents chat              Textual TUI (Python; rich UI)
echo   dagents tui [--plain]       Go bubbletea TUI (default full-screen; --plain for line REPL)
echo   dagents node                Start Agent Node (foreground)
echo   dagents register-center     Start Register Center (optional A2A)
echo   dagents doctor              Check installed files
echo   dagents version             Print version information
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
