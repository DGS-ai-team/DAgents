# `desktop/tray/` — Windows Desktop Shell（Go 兼容轨）

**Windows 专用**托盘 Shell（Go + 系统浏览器），供安装包「**兼容模式**」使用。

> **推荐轨** → [`desktop/tray-tauri/`](../tray-tauri/)（Win10/11 + WebView2，内嵌 Web UI）  
> CI 产物：`dist/dagents-shell-legacy.exe`（`scripts/ci/build_dagents_shell.sh`）  
> 安装时写入 `bin\dagents-shell.exe`（与 Tauri 轨互斥二选一）

Go 兼容轨与 Tauri 推荐轨共享 Node 平台 bridge、SSE 待办事件和 token 认证契约；本轨只保留
Windows 原生浏览器、托盘通知和安装器适配。功能与菜单的历史背景见
[`docs/archive/design/windows-desktop-shell.md`](../../docs/archive/design/windows-desktop-shell.md)。
