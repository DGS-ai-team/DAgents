@echo off
setlocal EnableExtensions
cd /d "%~dp0.."

where docker >nul 2>&1
if errorlevel 1 (
  echo [restart] error: docker not found >&2
  exit /b 1
)

if not exist ".env" (
  if exist ".env.example" (
    copy /Y ".env.example" ".env" >nul
    echo [restart] created .env from .env.example
  ) else (
    echo [restart] error: missing .env and .env.example >&2
    exit /b 1
  )
)

if not exist "docker-compose.yml" (
  echo [restart] error: missing docker-compose.yml >&2
  exit /b 1
)

docker compose version >nul 2>&1
if errorlevel 1 (
  docker-compose version >nul 2>&1
  if errorlevel 1 (
    echo [restart] error: docker compose not found >&2
    exit /b 1
  )
  for /f "delims=" %%P in ('docker-compose -f docker-compose.yml ps -q manage 2^>nul') do set "RUNNING=1"
  if defined RUNNING (
    echo [restart] restarting manage
    docker-compose -f docker-compose.yml restart manage
  ) else (
    echo [restart] starting manage
    docker-compose -f docker-compose.yml up -d
  )
) else (
  for /f "delims=" %%P in ('docker compose -f docker-compose.yml ps -q manage 2^>nul') do set "RUNNING=1"
  if defined RUNNING (
    echo [restart] restarting manage
    docker compose -f docker-compose.yml restart manage
  ) else (
    echo [restart] starting manage
    docker compose -f docker-compose.yml up -d
  )
)

if errorlevel 1 exit /b 1
echo [restart] health: curl -sf http://127.0.0.1:8020/health
echo [restart] console: http://127.0.0.1:8020/console/
endlocal
