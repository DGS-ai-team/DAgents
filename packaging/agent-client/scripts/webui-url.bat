@echo off
setlocal EnableDelayedExpansion
set "CFG=%~1"
if "%CFG%"=="" set "CFG=config.yaml"
set "PORT=18765"
if exist "%CFG%" (
  for /f "tokens=2 delims=: " %%P in ('findstr /R "^  port:" "%CFG%"') do (
    set "PORT=%%P"
    goto :done
  )
)
:done
echo http://127.0.0.1:!PORT!/ui/
