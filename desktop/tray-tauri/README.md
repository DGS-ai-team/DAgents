# `desktop/tray-tauri/` — DAgents Shell（Tauri 托盘）

用 **Tauri 2** 实现的 Windows 桌面托盘 Shell，目标逐步替代 Go 版 `desktop/tray`（`dagents-shell.exe`）。

## 为何用 Tauri

- 系统托盘 **双击** 为一级事件（打开 Web UI），无需 fork/修补 `getlantern/systray`
- 后续可加现代设置窗、通知与更新 UI，而不绑死 Win32 菜单
- 与仓库内 `packaging/bootstrapper`（安装向导）同技术栈

## 操作系统与运行时要求

官方（Tauri 2）桌面目标摘要：

| 平台 | 版本 / 运行时 |
|------|----------------|
| **Windows** | 文档写「Windows 7+」；**实际推荐 Windows 10/11**。渲染依赖 **Microsoft Edge WebView2**（Evergreen）。Win7/8 仅能用到 WebView2 ≤109，且 Tauri **≥2.12 不再保证** Win7 支持。 |
| **macOS** | Catalina **10.15+**（WKWebView） |
| **Linux** | 需 **webkit2gtk 4.1**（如 Ubuntu 22.04+）及 AppIndicator 等开发依赖 |

本仓库的 Shell **产品目标是 Windows 安装态**（与 Go Shell 相同）：

- **构建**：Windows + MSVC（C++ Build Tools）+ Rust + Node.js；CI 使用 `windows-2022`
- **运行**：Windows 10/11 + 已安装 WebView2（Win11 与多数 Win10 已预装；否则装 [Evergreen Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)）
- 托盘壳主界面默认隐藏，但仍通过 WebView2/Tauri 运行时加载；无 WebView2 时进程可能无法正常启动

参考：[Tauri Prerequisites](https://v2.tauri.app/start/prerequisites/)、[Drop Windows 7 support](https://github.com/tauri-apps/tauri/issues/12550)。

## 本版已实现

| 能力 | 说明 |
|------|------|
| 单实例 | Windows 命名 Mutex；其它平台 `.runtime/shell.lock` |
| 启动 ensure Node / 退出 stop Node | 对齐 Go `nodectl` 路径约定 |
| 托盘菜单 | 状态、打开控制台、启动/停止/重启、退出 |
| **双击托盘图标** | 与「打开控制台」相同：ensure Node → 打开 `{local.endpoint}/ui/` |
| health 轮询 + 自动恢复 | 用户手动「停止」后不自动拉起 |

## 尚未迁移（仍用 Go Shell）

待办 HITL / SSE、Toast 深链、更新检查与 Desktop API、路径粘贴 clipboard 等。详见 [`desktop/tray/README.md`](../tray/README.md)。

## 路径约定

与 Go Shell 相同：

```text
<安装根>/
  bin/dagents-shell.exe        # 安装包默认仍为 Go Shell
  bin/dagents-shell-tauri.exe  # CI 预览产物（可手动替换验证）
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
# 指定配置（相对安装根或绝对路径）
dagents-shell --config config.yaml
```

## 构建

本地：

```bash
cd desktop/tray-tauri
npm install
npm run tauri:build
```

CI 脚本（Windows runner）：

```bash
OUT_DIR=dist bash scripts/ci/build_dagents_shell_tauri.sh
# → dist/dagents-shell-tauri.exe（--bundles none，不打 NSIS）
```

产物也可在 `src-tauri/target/release/dagents-shell.exe`。

## CI

| Workflow | 行为 |
|----------|------|
| [`tauri-shell.yml`](../../.github/workflows/tauri-shell.yml) | PR/`dev`/`main` 变更 `desktop/tray-tauri/**` 时：Linux `cargo test` + Windows 构建并上传 artifact |
| `build-and-release.yml` / `manual-package.yml` | Windows 矩阵额外构建 `dagents-shell-tauri.exe` 并上传；Release 附带该预览二进制。**安装包仍嵌入 Go Shell** |

## 与 Go Shell 切换

1. 从 CI artifact 或本地构建得到 `dagents-shell-tauri.exe`
2. 备份安装目录中现有 `bin/dagents-shell.exe`
3. 复制为 `bin/dagents-shell.exe` 后执行 `dagents.cmd shell --background`

回滚：还原 Go 版二进制即可。
