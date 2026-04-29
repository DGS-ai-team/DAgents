@echo off
setlocal

cd /d "%~dp0"

if not exist ".env" (
  echo [startup] .env 不存在，将使用 .env.example 与环境变量默认值。
  echo [startup] 如需自定义配置，请先复制：copy .env.example .env
)

if not exist "dagents-api.exe" (
  echo [startup] 未找到可执行文件 dagents-api.exe
  exit /b 1
)

echo [startup] starting dagents-api (host/port 由 .env 或程序默认值决定)
dagents-api.exe
exit /b %ERRORLEVEL%
