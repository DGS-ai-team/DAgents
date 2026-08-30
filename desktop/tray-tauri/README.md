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
| SSE 待办 / Toast / icon 闪烁 | 对齐原 Go Shell |
| Desktop API `:18767` | update / clipboard / ui focus |
| 更新编排 + `update` CLI | Manage Release Hub |

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
