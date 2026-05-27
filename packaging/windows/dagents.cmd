@echo off
setlocal EnableExtensions

rem Use "%~dp0." for pushd: a trailing backslash before the closing quote breaks CMD parsing.
rem SHIFT changes %%1-%%9 only; %%* is unchanged. Never run: shift & "exe subcmd" %%* (duplicates subcmd).
pushd "%~dp0." >nul 2>&1
if errorlevel 1 goto dagents_pushd_failed
set "DAGENTS_HOME=%CD%"

if not exist ".env" if exist ".env.example" copy /Y ".env.example" ".env" >nul

if "%~1"=="" goto cli_default_chat
if /I "%~1"=="help" goto help
if /I "%~1"=="--help" goto run_cli
if /I "%~1"=="-h" goto run_cli
goto run_cli

:cli_default_chat
if not exist "dagents-cli.exe" goto missing_cli
dagents-cli.exe chat
goto cli_exit

:run_cli
if not exist "dagents-cli.exe" goto missing_cli
dagents-cli.exe %*
goto cli_exit

:missing_cli
echo [dagents] dagents-cli.exe was not found in "%DAGENTS_HOME%"
popd >nul
exit /b 1

:cli_exit
set "EXIT_CODE=%ERRORLEVEL%"
popd >nul
exit /b %EXIT_CODE%

:help
echo Usage:
echo   dagents chat              Start interactive Textual TUI chat
echo   dagents show session      List active and persisted sessions
echo   dagents delete session ID Delete persisted session not in queue
echo   dagents serve             Start Agent API in background (logs/dagents-api.log)
echo   dagents serve --stop      Stop background Agent API (+ shutdown.d hooks)
echo   dagents serve --status    Show background Agent API status
echo   dagents serve --foreground  Run Agent API in foreground (debug)
echo   dagents register-center   Start the Register Center
echo   dagents doctor            Check installed files
echo   dagents version           Print version information
echo.
echo Environment:
echo   Set DAGENTS_API_BASE to choose the chat backend URL.
echo   Copy .env.example to .env in "%DAGENTS_HOME%" to customize settings.
popd >nul
exit /b 0

:dagents_pushd_failed
echo [dagents] Cannot access install directory: "%~dp0."
exit /b 1
