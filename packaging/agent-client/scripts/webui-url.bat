@echo off
setlocal EnableExtensions
set "DAGENTS_WEBUI_CFG=%~1"
if "%DAGENTS_WEBUI_CFG%"=="" set "DAGENTS_WEBUI_CFG=config.yaml"
set "PORT=18765"
if exist "%DAGENTS_WEBUI_CFG%" (
  for /f "usebackq delims=" %%P in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "$ErrorActionPreference='SilentlyContinue'; $path=$env:DAGENTS_WEBUI_CFG; if (-not (Test-Path -LiteralPath $path)) { exit 0 }; $inListen=$false; Get-Content -LiteralPath $path | ForEach-Object { if ($_ -match '^listen:\s*$') { $inListen=$true; return }; if ($inListen -and $_ -match '^[^\s#]') { $inListen=$false }; if ($inListen -and $_ -match '^\s+port:\s*(\d+)') { Write-Output $matches[1]; exit 0 } }"`) do set "PORT=%%P"
)
echo http://127.0.0.1:%PORT%/ui/
