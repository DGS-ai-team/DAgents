# `scripts/windows/`

Windows 专用辅助脚本（托盘、快捷启动）。

| 文件 | 说明 |
|------|------|
| **`tray_launcher.py`** | 系统托盘启动器：后台拉起 `run_agent_api.py` / 可选 `run_register_center.py`，菜单启停、打开浏览器与安装目录 |
| **`start_tray.bat`** | 从仓库根调用 `.venv` 启动托盘（可传 `--with-register-center`） |

## 依赖

```bat
cd <DAgents 根目录>
.venv\Scripts\pip install -r requirements.txt -r requirements-windows-tray.txt
```

## 用法

```bat
REM 仅 Agent API（托盘启动时自动后台运行 API）
scripts\windows\start_tray.bat

REM 同时后台运行 Register Center
scripts\windows\start_tray.bat --with-register-center
```

或：

```bat
python scripts\windows\tray_launcher.py
```

## 日志

子进程 stdout/stderr 追加写入仓库根 **`logs/tray-api.log`**、**`logs/tray-register-center.log`**。

## 说明

- 工作目录固定为**仓库根**，以便读取 **`.env`** 与 **`.runtime/`**。
- 托盘图标为脚本内生成的简易位图（蓝底 “D”），无需额外 `.ico` 文件。
- 仅支持 **Windows**；Linux/macOS 请用 `nohup` / `systemd` 等方式（见根目录 `README.md`）。
