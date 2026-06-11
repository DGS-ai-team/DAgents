@echo off
setlocal
cd /d "%~dp0..\.."

if not exist "dagents.cmd" (
  echo [startup] 未找到 dagents.cmd
  exit /b 1
)

rem 与 packaging/linux/dagents 默认一致：后台启动并等待 probe 就绪；前台调试请用 dagents node --foreground
if not "%~1"=="" set "DAGENTS_CONFIG=%~1"
call dagents.cmd node
exit /b %ERRORLEVEL%
