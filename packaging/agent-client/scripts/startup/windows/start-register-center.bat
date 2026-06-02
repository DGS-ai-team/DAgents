@echo off
setlocal
cd /d "%~dp0..\.."

if not exist ".env" if exist ".env.example" (
  echo [startup] .env 不存在，将使用 .env.example 与环境变量默认值。
  echo [startup] 如需自定义: copy .env.example .env
)
if not exist "bin\dagents_register_center.exe" (
  echo [startup] 未找到 bin\dagents_register_center.exe
  exit /b 1
)

echo [startup] starting dagents_register_center
bin\dagents_register_center.exe
exit /b %ERRORLEVEL%
