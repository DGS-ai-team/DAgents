@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0.."

if not exist "image\" (
  echo [import-image] error: missing image\ directory >&2
  exit /b 1
)

where docker >nul 2>&1
if errorlevel 1 (
  echo [import-image] error: docker not found >&2
  exit /b 1
)

set "IMAGE_TAR="
if exist "VERSION" (
  set /p BUNDLE_VERSION=<VERSION
  if exist "image\dagents-manage-!BUNDLE_VERSION!.tar.gz" (
    set "IMAGE_TAR=image\dagents-manage-!BUNDLE_VERSION!.tar.gz"
  )
)

if not defined IMAGE_TAR (
  for %%F in (image\dagents-manage-*.tar.gz) do (
    if not defined IMAGE_TAR set "IMAGE_TAR=%%F"
  )
)

if not defined IMAGE_TAR (
  echo [import-image] error: no dagents-manage-*.tar.gz under image\ >&2
  exit /b 1
)

echo [import-image] loading !IMAGE_TAR!
docker load -i "!IMAGE_TAR!"
if errorlevel 1 exit /b 1

echo [import-image] done. verify: docker image ls dagents-manage
echo [import-image] next: scripts\restart.bat
endlocal
