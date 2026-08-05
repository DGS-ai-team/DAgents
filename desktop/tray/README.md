# `desktop/tray/` — Windows Desktop Shell（Go 兼容轨）

**Windows 专用**托盘 Shell（Go + 系统浏览器），供安装包「**兼容模式**」使用。

> **推荐轨** → [`desktop/tray-tauri/`](../tray-tauri/)（Win10/11 + WebView2，内嵌 Web UI）  
> CI 产物：`dist/dagents-shell-legacy.exe`（`scripts/ci/build_dagents_shell.sh`）  
> 安装时写入 `bin\dagents-shell.exe`（与 Tauri 轨互斥二选一）

功能与菜单说明见历史实现与 [`docs/design/windows-desktop-shell.md`](../../docs/design/windows-desktop-shell.md)。
