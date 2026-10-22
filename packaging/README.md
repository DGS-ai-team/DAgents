# `packaging/`

分发辅助说明（非运行时模块），可随内网拷贝包附带给用户。

## 制品命名约定

| 名称 | 含义 |
|------|------|
| **`dagents-local-assistant-linux-amd64-*.tar.gz`** | Linux Go Node API、`dagents-client`（probe/update）、`dagents-browser`、bootstrap 配置、`.runtime/`、Agent templates 与启动脚本；不包含 Desktop UI |
| **`dagents-manage-bundle-*.tar.gz`** | Manage **离线包**（唯一 Manage Release 附件）：内含镜像 + `docker-compose` + 导入/重启脚本 |
| **`dagents-manage-*.tar.gz`**（本地） | 纯镜像导出；仅 `build_manage_docker.sh` 中间产物，**不再单独发布** |
| **`dagents-backend-*`**（legacy） | 旧 Python 全栈后端；**Release CI 已不再构建** |
| **`dagents-cli`**（removed） | 原 Textual TUI；Phase 4 起移除 |

| **`dagents-local-assistant-windows-amd64-installer-*.exe`** | Windows **x64 Inno** 安装包（Agent Node + Tauri Desktop） |
| **`dagents-local-assistant-windows-386-installer-*.exe`** | Windows **x86/32 位 Inno** 安装包（Agent Node + Tauri Desktop） |
| **`DAgents Setup`（removed）** | 原便携 Tauri Setup 向导已移除；请直接使用 Inno 安装包 |

本地/CI 打包：

```bash
# 当前操作系统（需 Go + Python 3.11+）
scripts/package_local_assistant.sh

# 产物示例
# dist/dagents-local-assistant-linux-amd64-0.5.1.tar.gz
# dist/dagents-local-assistant-windows-amd64-installer-0.5.1.exe
# dist/dagents-local-assistant-windows-386-installer-0.5.1.exe
```

Linux Release CI：Runner **ubuntu-latest**；Go 二进制 **CGO_ENABLED=0** 静态编译，不需要构建前端；Linux Node 包只提供 API，不包含 Desktop Runtime。Manage Console 由 **`manage/console/build.sh`** 或 Docker 多阶段构建。Windows Release CI 在 x64/x86 矩阵中构建 Node 与 Tauri Desktop 并生成 Inno 安装包；Go browser-based Shell 仅保留源码，不发行。`dagents-cli`（Textual TUI）已移除。

### Desktop UI 与 Node package

- Windows 桌面界面由独立 **Tauri Desktop** 提供；Node 只提供 API，不再托管 `/ui/` 浏览器应用。
- Linux Node archive 是 API 服务包，不包含 Desktop UI 或其他桌面运行时。
- `desktop/tray-tauri/src/` 由 Desktop 自身构建；`shared/frontend/` 仅保留跨产品共享的展示 helper。Go Node 构建不依赖前端源码或静态资源。

| 工作流 | 触发 | 产物 |
|--------|------|------|
| [build-and-release.yml](../.github/workflows/build-and-release.yml) | 推送 **`v*`** 标签 | GitHub Release + Linux tar.gz + Windows x64/x86 **Inno `.exe`** + **`dagents-manage-bundle-*.tar.gz`** |
| [manual-package.yml](../.github/workflows/manual-package.yml) | 手动 **workflow_dispatch** | Actions Artifact（Linux tar.gz + Windows x64/x86 安装包） |
| [go-ac.yml](../.github/workflows/go-ac.yml) | PR / push（Go 路径） | 仅测试与编译冒烟，不打包 |

| 路径 | 说明 |
|------|------|
| **`agent-client/`** | Node bootstrap 示例（`config.example.yaml` 仅含 `listen`）；独立 Client 的 `local` endpoint 配置仍由各 Client 自己管理 |
| **`agent-templates/`** | 内置 Agent 模板（含 soul/custom 预设；打入发布包，并 `go:embed` 进 Node） |
| **`runtime/`** | 预编译包内 **`.runtime/`** 占位（policy、skills、prompt_context 等；**`RECOMMENDED_CLI_TOOLS.md`** 推荐第三方 CLI） |
| **`linux/`** | Linux **`dagents`** 启动脚本 + **`install.sh`**（打入 tar.gz 根目录） |
| **`windows/`** | Inno Setup 安装包（`dagents-installer.iss` + 分步配置向导 + `write-install-config.ps1`） |
| **`manage/`** | **Manage 控制面 Docker 镜像**（Registry + Workgroup + Console；见 [`manage/README.md`](manage/README.md)） |
| **`OFFLINE_INSTALL.md`** | 源码离线安装（开发/调试） |

架构与联调见 [docs/architecture.md](../docs/architecture.md) 与 [docs/development.md](../docs/development.md)。

## 推荐 CLI 工具

发布包**不内置**第三方 CLI。推荐清单（含 **[OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)** 安装与 skills 同步）见 [`runtime/RECOMMENDED_CLI_TOOLS.md`](runtime/RECOMMENDED_CLI_TOOLS.md)。

Windows 安装包 / Linux **`install.sh`** 会将 **`.runtime/externaltools`** 加入 `PATH`，便于放置自行下载的工具。
