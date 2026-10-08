# `desktop/tray-tauri/` — DAgents Shell（Tauri，推荐轨）

用 **Tauri 2** 实现的 Windows 桌面托盘 Shell（内嵌 WebView2）。Windows x64/x86 Inno 安装包的「**推荐**」Shell 均使用本实现；发布不再提供免安装 zip。

## 与兼容轨的关系

| 轨 | 目录 | CI 产物 | 适用 |
|----|------|---------|------|
| **推荐** | `desktop/tray-tauri/` | `dagents-shell-tauri.exe` | Win10/11 + WebView2，内嵌 Web UI |
| **兼容** | [`desktop/tray/`](../tray/) | `dagents-shell-legacy.exe` | 低版本 Windows，系统浏览器开 Web UI |

Inno 安装向导在「附加任务」中二选一；静默安装可用 `/TASKS=shellmodern` 或 `/TASKS=shelllegacy`。未检测到 WebView2 时向导默认勾选兼容模式。

## 已实现

| 能力 | 说明 |
|------|------|
| 单实例 / Node 启停监护 | 对齐 Go `nodectl` |
| 双击托盘 / 关窗隐藏 | 内嵌 WebView 打开 `{endpoint}/ui/` |
| SSE 待办 / Toast / icon 闪烁 | 订阅 Node 权威事件；启动/重连时 hydrate |
| Desktop bridge `:18767` | 仅供 Node 调用的 update / clipboard / ui focus API；目录选择由 Node 直接负责 |
| 更新编排 + `update` CLI | Manage Release Hub |

### Desktop（当前主入口）

Shell 启动后默认打开 Desktop 窗口；托盘菜单中的「打开控制台」仍进入 Node `/ui/`，用于首配和高级管理。启动参数 `--legacy-console` 可在诊断时跳过 Desktop。Desktop 通过 Rust Tauri commands 访问本机 Node；认证凭据由 Rust 宿主持有，WebView 可显示连接地址但不直接请求 Node。当前只接受 loopback Node（可用 SSH 隧道转发到 loopback），不接受 LAN/WAN 地址。对话、审批和终端资源已接入真实 Node API；浏览器、文件和工作组仍明确未接入。终端通过 Tauri Rust 中继到 Node，关闭会话会移除 PTY，切换 Agent 或退出窗口会 detach 并保留进程。

Desktop「设置」复用 Node WebUI 的完整配置页面，通过固定本机 Node 的 Tauri API 通道读取和保存通用、模型、工具、智能体、触发器、安全、反馈等设置。Node 仍是配置数据的权威来源；Desktop 不另存配置副本。

本轮范围、实际测试与下一阶段缺口见 [Desktop MVP 收束记录](../../docs/design/desktop-product/mvp-closeout.md)。当前仍为本机开发 MVP，尚未完成资源预览、全部日常对话操作和多平台发布验收。

本地 Windows 开发：

```powershell
$env:DAGENTS_HOME = (Resolve-Path .).Path
$env:DAGENTS_CONFIG = Join-Path $env:DAGENTS_HOME 'packaging/agent-client/config.yaml'
cd desktop/tray-tauri
npm install
npm run tauri:dev
```

参数需传给 Tauri 子进程。Node 已在默认端口 `127.0.0.1:18765` 运行时，Shell 复用该进程；尚未完成 Node 首配时，MVP 会引导用户通过原 `/ui/` 控制台完成配置。单独 `npm run dev` 可在浏览器查看排版预览，但会明确显示预览状态，不能连接或伪装真实 Node 数据。

Web UI 不直接访问 `:18767`。Shell 启动时生成 `.runtime/desktop-bridge.token`，Node 继承
`DAGENTS_DESKTOP_API_URL` 与 `DAGENTS_DESKTOP_BRIDGE_TOKEN` 后，通过
`/v1/platform/*` 统一转发桌面能力；Go 兼容轨使用相同契约。

## 开发

```bash
cd desktop/tray-tauri
npm install
npm run tauri:dev
```

```bash
# 仓库根目录（Windows）
scripts/ci/build_dagents_shell_tauri.sh   # → dist/dagents-shell-tauri.exe + dagents-shell.exe
scripts/ci/build_dagents_shell.sh         # → dist/dagents-shell-legacy.exe（Go）
```

## CI

| Workflow | 作用 |
|----------|------|
| [`tauri-shell.yml`](../../.github/workflows/tauri-shell.yml) | `cargo test` + 构建 Tauri 产物 |
| `build-and-release.yml` / `manual-package.yml` | 同时构建 Tauri + Go legacy，打入安装包 |
