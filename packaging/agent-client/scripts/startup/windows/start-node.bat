@echo off
setlocal
cd /d "%~dp0..\.."

set "CFG=%~1"
if "%CFG%"=="" set "CFG=config.yaml"
if not exist "%CFG%" (
  echo [startup] 未找到 %CFG%，请先: copy config.example.yaml config.yaml
  exit /b 1
)
if not exist "bin\dagents-node.exe" (
  echo [startup] 未找到 bin\dagents-node.exe
  exit /b 1
)

echo [startup] starting dagents-node -config %CFG%
bin\dagents-node.exe -config %CFG%
exit /b %ERRORLEVEL%
