# `desktop/tray-tauri/` — DAgents Shell（Tauri 托盘）

用 **Tauri 2** 实现的 Windows 桌面托盘 Shell，目标逐步替代 Go 版 `desktop/tray`（`dagents-shell.exe`）。

## 为何用 Tauri

- 系统托盘 **双击** 为一级事件（打开 Web UI），无需 fork/修补 `getlantern/systray`
- 后续可加现代设置窗、通知与更新 UI，而不绑死 Win32 菜单
- 与仓库内 `packaging/bootstrapper`（安装向导）同技术栈

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
  bin/dagents-shell.exe   # 本工程 release 产物名同为 dagents-shell
  bin/dagents-node.exe
  config.yaml
  .runtime/node.pid
  .runtime/logs/...
```

`DAGENTS_HOME` → 可执行文件在 `bin/` 时取父目录 → 当前工作目录。

## 开发

需：Node.js、Rust、（Windows 上）WebView2。

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

```bash
cd desktop/tray-tauri
npm install
npm run tauri:build
```

产物在 `src-tauri/target/release/dagents-shell`（Windows 为 `.exe`）。可拷贝到安装根 `bin/`，替代或并行于 Go Shell 验证。

Linux CI **可** `cargo check`；完整托盘交互需在 Windows 上验证。

## 与 Go Shell 切换

1. 构建本工程得到 `dagents-shell.exe`
2. 备份安装目录中现有 `bin/dagents-shell.exe`
3. 替换后执行 `dagents.cmd shell --background`

回滚：还原 Go 版二进制即可。打包流水线默认仍构建 Go Shell，待本工程功能对齐后再切 CI。
