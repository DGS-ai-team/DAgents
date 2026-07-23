# `packaging/`

分发辅助说明（非运行时模块），可随内网拷贝包附带给用户。

## 制品命名约定

| 名称 | 含义 |
|------|------|
| **`dagents-local-assistant-*.{tar.gz,zip}`** | **Go Node + 双 TUI + 内嵌 Web UI**：`dagents-node`（含 `go:embed` 的 `/ui/` 静态资源）、`dagents-client`、`dagents-cli` + 配置、`.runtime/`、**`scripts/`** |
| **`dagents-manage-*.tar.gz`** | Manage Docker 镜像导出（Registry + A2A + Console） |
| **`dagents-manage-bundle-*.tar.gz`** | Manage **离线包**：镜像 + `docker-compose` + 导入/重启脚本 |
| **`dagents-backend-*`**（legacy） | 旧 Python 全栈后端；**Release CI 已不再构建** |
| **`dagents-cli`** | Textual TUI 单文件二进制（PyInstaller 产物名） |

| **`dagents-local-assistant-*-installer-*.exe`** | Windows Inno 安装包 |
| **`DAgents Setup`（可选）** | Tauri 现代向导，见 [`bootstrapper/`](./bootstrapper/)；内嵌上述 Inno 包静默安装 |

本地/CI 打包：

```bash
# 当前操作系统（需 Go + Python 3.11+）
scripts/package_local_assistant.sh

# 产物示例
# dist/dagents-local-assistant-linux-amd64-0.5.1.tar.gz
# dist/dagents-local-assistant-windows-amd64-0.5.1.zip
```

Linux Release CI：Runner **ubuntu-latest**；`dagents-cli` 在 **rockylinux:8**（glibc 2.28）容器内 PyInstaller；Go 二进制 **CGO_ENABLED=0** 静态编译；**Release 构建前执行 `node/webui/build.sh`**（Web UI 不入库，嵌入 `dagents-node`）；Manage Console 由 **`manage/console/build.sh`** 或 Docker 多阶段构建。

### Web UI 与安装包

- **无需单独 UI 安装包**：浏览器 Client 静态资源通过 `go:embed` 打进 **`dagents-node`**，启动 Node 后访问 `http://127.0.0.1:<port>/ui/`。
- **开发**时可用 Vite 热更新（见 [`node/webui/README.md`](../node/webui/README.md)）；**生产/Release** 须在 `go build` 前运行 `bash node/webui/build.sh`（CI 已自动化；静态产物不提交 Git）。
- 若 `ui.enabled: false`，Node 不挂载 `/ui/` 路由；终端 Client（TUI）不受影响。

| 工作流 | 触发 | 产物 |
|--------|------|------|
| [build-and-release.yml](../.github/workflows/build-and-release.yml) | 推送 **`v*`** 标签 | GitHub Release + `dagents-local-assistant-*` + **Windows `.exe` 安装包** + **`dagents-manage-*.tar.gz`** + **`dagents-manage-bundle-*.tar.gz`** |
| [manual-package.yml](../.github/workflows/manual-package.yml) | 手动 **workflow_dispatch** | Actions Artifact（zip/tar.gz + Windows 安装包） |
| [go-ac.yml](../.github/workflows/go-ac.yml) | PR / push（Go 路径） | 仅测试与编译冒烟，不打包 |

| 路径 | 说明 |
|------|------|
| **`agent-client/`** | Go Node + Client **共用 YAML** 示例（`config.example.yaml`、`agent-card.example.json`） |
| **`runtime/`** | 预编译包内 **`.runtime/`** 占位（policy、skills、prompt_context 等；**`RECOMMENDED_CLI_TOOLS.md`** 推荐第三方 CLI） |
| **`linux/`** | Linux **`dagents`** 启动脚本 + **`install.sh`**（打入 tar.gz 根目录） |
| **`windows/`** | Inno Setup 安装包（`dagents-installer.iss` + 分步配置向导 + `write-install-config.ps1`） |
| **`manage/`** | **Manage 控制面 Docker 镜像**（Registry + A2A + Console；见 [`manage/README.md`](manage/README.md)） |
| **`OFFLINE_INSTALL.md`** | 源码离线安装（开发/调试） |

架构与联调见 [local-assistant.md](../docs/architecture/local-assistant.md)。

## 推荐 CLI 工具

发布包**不内置**第三方 CLI。推荐清单（含 **[OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)** 安装与 skills 同步）见 [`runtime/RECOMMENDED_CLI_TOOLS.md`](runtime/RECOMMENDED_CLI_TOOLS.md)。

Windows 安装包 / Linux **`install.sh`** 会将 **`.runtime/externaltools`** 加入 `PATH`，便于放置自行下载的工具。
