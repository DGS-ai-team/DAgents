@echo off
setlocal

cd /d "%~dp0"

if not exist ".env" (
  echo [startup] .env 不存在，将使用 .env.example 与环境变量默认值。
  echo [startup] 如需自定义配置，请先复制：copy .env.example .env
)

if not exist "dagents-register-center.exe" (
  echo [startup] 未找到可执行文件 dagents-register-center.exe
  exit /b 1
)

if "%REGISTER_CENTER_HOST%"=="" set REGISTER_CENTER_HOST=0.0.0.0
if "%REGISTER_CENTER_PORT%"=="" set REGISTER_CENTER_PORT=8010

echo [startup] starting dagents-register-center on %REGISTER_CENTER_HOST%:%REGISTER_CENTER_PORT%
dagents-register-center.exe
exit /b %ERRORLEVEL%
