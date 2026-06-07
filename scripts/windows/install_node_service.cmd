@echo off
setlocal EnableExtensions EnableDelayedExpansion

REM 将 dagents-node 注册为 SYSTEM 开机计划任务（DAgents\AgentNode）。
REM Node 未实现 Windows SCM，非 Services.msc 服务。
REM
REM 用法（仓库根目录，管理员 CMD）：
REM   scripts\windows\install_node_service.cmd install [config.yaml] [--build] [--start-now]
REM   scripts\windows\install_node_service.cmd uninstall
REM   scripts\windows\install_node_service.cmd status

set "ACTION=%~1"
if "%ACTION%"=="" goto usage

set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..\..") do set "REPO_ROOT=%%~fI"
set "TASK_NAME=DAgents\AgentNode"
set "WRAPPER_DIR=%REPO_ROOT%\.runtime\service\windows"
set "WRAPPER=%WRAPPER_DIR%\dagents-node-run.cmd"

set "CONFIG_ARG="
set "DO_BUILD=0"
set "START_NOW=0"

if /I "%ACTION%"=="install" goto parse_install
if /I "%ACTION%"=="uninstall" goto do_uninstall
if /I "%ACTION%"=="status" goto do_status
goto usage

:parse_install
shift
:parse_loop
if "%~1"=="" goto after_parse
if /I "%~1"=="--build" (
  set "DO_BUILD=1"
  shift
  goto parse_loop
)
if /I "%~1"=="--start-now" (
  set "START_NOW=1"
  shift
  goto parse_loop
)
if not defined CONFIG_ARG (
  set "CONFIG_ARG=%~1"
  shift
  goto parse_loop
)
echo [error] unknown option: %~1
exit /b 1

:after_parse
call :resolve_config
if errorlevel 1 exit /b 1
call :resolve_binary
if errorlevel 1 exit /b 1
call :write_wrapper
if errorlevel 1 exit /b 1
call :create_task
if errorlevel 1 exit /b 1
if "%START_NOW%"=="1" (
  schtasks /Run /TN "%TASK_NAME%"
)
echo [install] wrapper: %WRAPPER%
echo [install] Windows 侧为开机计划任务（非 Services.msc 服务）
exit /b 0

:resolve_config
if defined CONFIG_ARG (
  if exist "%CONFIG_ARG%" (
    for %%F in ("%CONFIG_ARG%") do set "CONFIG=%%~fF"
    exit /b 0
  )
  echo [error] config not found: %CONFIG_ARG%
  exit /b 1
)
if defined DAGENTS_CONFIG (
  if exist "%DAGENTS_CONFIG%" (
    set "CONFIG=%DAGENTS_CONFIG%"
    exit /b 0
  )
  echo [error] DAGENTS_CONFIG not found: %DAGENTS_CONFIG%
  exit /b 1
)
set "C1=%REPO_ROOT%\config.yaml"
set "C2=%REPO_ROOT%\config.example.yaml"
set "C3=%REPO_ROOT%\packaging\agent-client\config.yaml"
set "C4=%REPO_ROOT%\packaging\agent-client\config.example.yaml"
if exist "%C1%" (set "CONFIG=%C1%" & exit /b 0)
if exist "%C2%" (set "CONFIG=%C2%" & exit /b 0)
if exist "%C3%" (set "CONFIG=%C3%" & exit /b 0)
if exist "%C4%" (set "CONFIG=%C4%" & exit /b 0)
echo [error] config not found: pass config path or set DAGENTS_CONFIG
exit /b 1

:resolve_binary
if "%DO_BUILD%"=="1" (
  echo [install] go build -o bin\dagents-node.exe .\node\cmd\dagents-node
  pushd "%REPO_ROOT%"
  go build -o bin\dagents-node.exe .\node\cmd\dagents-node
  if errorlevel 1 (
    popd
    exit /b 1
  )
  popd
  set "BINARY=%REPO_ROOT%\bin\dagents-node.exe"
  exit /b 0
)
if exist "%REPO_ROOT%\bin\dagents-node.exe" (
  set "BINARY=%REPO_ROOT%\bin\dagents-node.exe"
  exit /b 0
)
if exist "%REPO_ROOT%\dagents-node.exe" (
  set "BINARY=%REPO_ROOT%\dagents-node.exe"
  exit /b 0
)
where dagents-node.exe >nul 2>&1
if not errorlevel 1 (
  for /f "delims=" %%P in ('where dagents-node.exe') do (
    set "BINARY=%%P"
    goto binary_done
  )
)
:binary_done
if not defined BINARY (
  echo [error] dagents-node.exe not found: build first or pass --build
  exit /b 1
)
exit /b 0

:write_wrapper
if not exist "%WRAPPER_DIR%" mkdir "%WRAPPER_DIR%"
(
  echo @echo off
  echo setlocal
  echo set "DAGENTS_CONFIG=%CONFIG%"
  echo cd /d "%REPO_ROOT%"
  echo "%BINARY%" -config "%CONFIG%"
  echo endlocal
) > "%WRAPPER%"
exit /b 0

:create_task
echo [install] creating scheduled task: %TASK_NAME%
schtasks /Create /F /TN "%TASK_NAME%" /TR "\"%WRAPPER%\"" /SC ONSTART /RU SYSTEM /RL HIGHEST
if errorlevel 1 (
  echo [error] schtasks failed — 请以管理员身份运行 CMD
  exit /b 1
)
exit /b 0

:do_uninstall
schtasks /Delete /F /TN "%TASK_NAME%" >nul 2>&1
if errorlevel 1 (
  echo [uninstall] task not found or delete failed: %TASK_NAME%
) else (
  echo [uninstall] removed task %TASK_NAME%
)
if exist "%WRAPPER%" (
  del /f /q "%WRAPPER%"
  echo [uninstall] removed %WRAPPER%
)
exit /b 0

:do_status
schtasks /Query /TN "%TASK_NAME%" /FO LIST /V
if errorlevel 1 (
  echo [status] task not found: %TASK_NAME%
  exit /b 1
)
exit /b 0

:usage
echo 用法:
echo   install_node_service.cmd install [config.yaml] [--build] [--start-now]
echo   install_node_service.cmd uninstall
echo   install_node_service.cmd status
exit /b 1
