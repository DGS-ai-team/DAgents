# `desktop/tray-tauri/` — DAgents Shell（Tauri 托盘）

用 **Tauri 2** 实现的 Windows 桌面托盘 Shell，为安装包中的 **`bin/dagents-shell.exe`**（唯一 Shell 实现）。

## 为何用 Tauri

- 系统托盘 **双击** 为一级事件（打开 Web UI），无需 fork/修补 `getlantern/systray`
- 内嵌 WebView2 加载 Node 同源 `/ui/`，关窗隐藏到托盘
- 与仓库内 `packaging/bootstrapper`（安装向导）同技术栈

## 操作系统与运行时要求

| 平台 | 版本 / 运行时 |
|------|----------------|
| **Windows** | **Windows 10/11** + **Microsoft Edge WebView2**（Evergreen） |
| **macOS / Linux** | 非产品目标；开发可用 `cargo test` |

本仓库的 Shell **产品目标是 Windows 安装态**：

- **构建**：Windows + MSVC + Rust + Node.js；CI 使用 `windows-2022`
- **运行**：需 WebView2（Win11 与多数 Win10 已预装）
- 托盘壳默认隐藏主窗口；**打开控制台时在内嵌 WebView 中加载** Node 同源 `{endpoint}/ui/`
- 关闭控制台窗口 → **隐藏到托盘**（不停 Node、不退出 Shell）

参考：[Tauri Prerequisites](https://v2.tauri.app/start/prerequisites/)。

## 已实现（对齐原 Go Shell）

| 能力 | 说明 |
|------|------|
| 单实例 | Windows 命名 Mutex；其它平台 `.runtime/shell.lock` |
| 启动 ensure Node / 退出 stop Node | 对齐 Go `nodectl` 路径约定 |
| 托盘菜单 | 状态、待办（最多 8 槽）、打开控制台/Manage、更新、启停重启、退出 |
| **双击托盘图标** | ensure Node → 主窗口 navigate `{endpoint}/ui/` → show/focus |
| 关窗隐藏 | CloseRequested → hide + skip taskbar |
| 导航白名单 | 仅允许配置的 Node endpoint、本地 stub、tauri/asset |
| health 轮询 + 自动恢复 | 用户手动「停止」后不自动拉起 |
| SSE + 待办同步 | `GET /v1/streams?live=1` + `/v1/agents` 兜底；Toast / icon 闪烁 |
| Desktop API `:18767` | update / clipboard / ui focus（Web UI 依赖） |
| 更新检查与编排 | Manage Release Hub + `dagents-shell update` CLI |
| Manage 打开 | Shell 带本机 `node_id` 打开 Console |

历史 Go 实现见已退役目录 [`desktop/tray/`](../tray/)。

## 路径约定

```text
<安装根>/
  bin/dagents-shell.exe        # Tauri Shell（唯一）
  bin/dagents-node.exe
  config.yaml
  .runtime/node.pid
  .runtime/logs/...
```

`DAGENTS_HOME` → 可执行文件在 `bin/` 时取父目录 → 当前工作目录。

## 开发

需：Node.js、Rust、（Windows 上）WebView2 + MSVC。

```bash
cd desktop/tray-tauri
npm install
npm run tauri:dev
```

可选参数：

```bash
dagents-shell --config config.yaml
dagents-shell update --check
```

## 构建

```bash
# 仓库根目录（Windows）
scripts/ci/build_dagents_shell_tauri.sh
# → dist/dagents-shell.exe
```

也可：`scripts/ci/build_dagents_shell.sh`（已委托上述脚本）。

## CI

| Workflow | 作用 |
|----------|------|
| [`tauri-shell.yml`](../../.github/workflows/tauri-shell.yml) | `cargo test` + Windows 构建 `dagents-shell.exe` |
| `build-and-release.yml` / `manual-package.yml` | Windows 矩阵构建 Tauri Shell 并打入安装包 |
