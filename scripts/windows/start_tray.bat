@echo off
setlocal
cd /d "%~dp0\..\.."

if not exist ".venv\Scripts\python.exe" (
  echo [tray] 未找到 .venv\Scripts\python.exe，请先在仓库根创建虚拟环境并安装依赖。
  echo [tray]   python -m venv .venv
  echo [tray]   .venv\Scripts\pip install -r requirements.txt -r requirements-windows-tray.txt
  exit /b 1
)

echo [tray] starting DAgents tray launcher...
.venv\Scripts\python.exe scripts\windows\tray_launcher.py %*
exit /b %ERRORLEVEL%
