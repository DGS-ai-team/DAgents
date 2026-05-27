@echo off
setlocal EnableExtensions

rem 安装目录：%~dp0 末尾自带 \，必须写成 "%~dp0." 再 pushd，否则 \" 会转义引号（报「此时不应有 \Backend\。」）。
rem 勿在 if (...) 块内 echo 未加引号的 %DAGENTS_HOME%：括号块解析时路径末尾 \ 仍会触发同样错误。
pushd "%~dp0." >nul 2>&1
if errorlevel 1 goto dagents_pushd_failed
set "DAGENTS_HOME=%CD%"

if "%~1"=="" goto chat
if /I "%~1"=="chat" goto chat_shift
if /I "%~1"=="serve" goto serve_shift
if /I "%~1"=="api" goto serve_shift
if /I "%~1"=="register-center" goto register_center_shift
if /I "%~1"=="doctor" goto doctor
if /I "%~1"=="version" goto version
if /I "%~1"=="help" goto help
if /I "%~1"=="--help" goto help
if /I "%~1"=="-h" goto help

echo [dagents] Unknown command: %~1
echo.
goto help_error

:chat_shift
shift /1
:chat
if not exist "dagents-cli.exe" (
  echo [dagents] dagents-cli.exe was not found in "%DAGENTS_HOME%"
  popd >nul
  exit /b 1
)
dagents-cli.exe chat %*
set "EXIT_CODE=%ERRORLEVEL%"
popd >nul
exit /b %EXIT_CODE%

:serve_shift
shift /1
if not exist "dagents-api.exe" (
  echo [dagents] dagents-api.exe was not found in "%DAGENTS_HOME%"
  popd >nul
  exit /b 1
)
dagents-api.exe %*
set "EXIT_CODE=%ERRORLEVEL%"
popd >nul
exit /b %EXIT_CODE%

:register_center_shift
shift /1
if not exist "dagents_register_center.exe" (
  echo [dagents] dagents_register_center.exe was not found in "%DAGENTS_HOME%"
  popd >nul
  exit /b 1
)
dagents_register_center.exe %*
set "EXIT_CODE=%ERRORLEVEL%"
popd >nul
exit /b %EXIT_CODE%

:doctor
echo DAgents installation: %DAGENTS_HOME%
if exist "dagents-cli.exe" (
  echo [ok] dagents-cli.exe found
) else (
  echo [missing] dagents-cli.exe
)
if exist "dagents-api.exe" (
  echo [ok] dagents-api.exe found
) else (
  echo [missing] dagents-api.exe
)
if exist "dagents_register_center.exe" (
  echo [ok] dagents_register_center.exe found
) else (
  echo [missing] dagents_register_center.exe
)
if exist ".env" (
  echo [ok] .env found
) else (
  echo [info] .env not found; defaults and .env.example will be used
)
if exist ".runtime" (
  echo [ok] .runtime found
) else (
  echo [missing] .runtime
)
popd >nul
exit /b 0

:version
if exist "dagents-cli.exe" (
  dagents-cli.exe version
  set "EXIT_CODE=%ERRORLEVEL%"
  popd >nul
  exit /b %EXIT_CODE%
)
echo DAgents
popd >nul
exit /b 0

:help
echo Usage:
echo   dagents chat              Start interactive Textual TUI chat
echo   dagents show session      List active and persisted sessions
echo   dagents delete session ID Delete persisted session not in queue
echo   dagents serve             Start the Agent API backend
echo   dagents register-center   Start the Register Center
echo   dagents doctor            Check installed files
echo   dagents version           Print version information
echo.
echo Environment:
echo   Set DAGENTS_API_BASE to choose the chat backend URL.
echo   Copy .env.example to .env in "%DAGENTS_HOME%" to customize host, port, model, and provider settings.
popd >nul
exit /b 0

:help_error
echo Usage:
echo   dagents chat
echo   dagents serve
echo   dagents register-center
echo   dagents doctor
echo   dagents version
popd >nul
exit /b 1

:dagents_pushd_failed
echo [dagents] Cannot access install directory: "%~dp0."
exit /b 1
