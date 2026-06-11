# `packaging/`

分发辅助说明（非运行时模块），可随内网拷贝包附带给用户。

## 制品命名约定

| 名称 | 含义 |
|------|------|
| **`dagents-local-assistant-*.{tar.gz,zip}`** | **Go Node + 双 TUI + Register Center**：`dagents-node`、`dagents-client`、`dagents-cli`、`dagents_register_center` + 配置、`.runtime/`、**`scripts/`**（启动与 systemd/计划任务注册） |
| **`dagents-backend-*`**（legacy） | 旧 Python 全栈后端（api + register_center + cli）；**Release CI 已不再构建** |
| **`dagents-cli`** | Textual TUI 单文件二进制（PyInstaller 产物名） |

本地/CI 打包：

```bash
# 当前操作系统（需 Go + Python 3.11+）
scripts/package_local_assistant.sh

# 产物示例
# dist/dagents-local-assistant-linux-amd64-0.2.2.tar.gz
# dist/dagents-local-assistant-windows-amd64-0.2.2.zip
```

Linux Release CI：Runner **ubuntu-latest**；`dagents-cli` / `dagents_register_center` 在 **rockylinux:8**（glibc 2.28）容器内 PyInstaller；Go 二进制 **CGO_ENABLED=0** 静态编译。

| 工作流 | 触发 | 产物 |
|--------|------|------|
| [build-and-release.yml](../.github/workflows/build-and-release.yml) | 推送 **`v*`** 标签 | GitHub Release + `dagents-local-assistant-*` + **Windows `.exe` 安装包** + **`dagents-manage-*.tar.gz`** |
| [manual-package.yml](../.github/workflows/manual-package.yml) | 手动 **workflow_dispatch** | Actions Artifact（zip/tar.gz + Windows 安装包） |
| [go-ac.yml](../.github/workflows/go-ac.yml) | PR / push（Go 路径） | 仅测试与编译冒烟，不打包 |

| 路径 | 说明 |
|------|------|
| **`agent-client/`** | Go Node + Client **共用 YAML** 示例（`config.example.yaml`） |
| **`runtime/`** | 预编译包内 **`.runtime/`** 占位（policy、skills、prompt_context 等；**`RECOMMENDED_CLI_TOOLS.md`** 推荐第三方 CLI） |
| **`linux/`** | Linux **`dagents`** 启动脚本 + **`install.sh`**（打入 tar.gz 根目录） |
| **`windows/`** | Inno Setup 安装包（`dagents-installer.iss` + `dagents.cmd`；Release Windows 矩阵构建 `.exe`） |
| **`manage/`** | **Manage 控制面 Docker 镜像**（Registry + A2A + Console；见 [`manage/README.md`](manage/README.md)） |
| **`OFFLINE_INSTALL.md`** | 源码离线安装（开发/调试） |

架构与联调见 [local-assistant.md](../docs/architecture/local-assistant.md)。

## 推荐 CLI 工具

发布包**不内置**第三方 CLI。推荐清单（含 **[OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)** 安装与 skills 同步）见 [`runtime/RECOMMENDED_CLI_TOOLS.md`](runtime/RECOMMENDED_CLI_TOOLS.md)。

Windows 安装包 / Linux **`install.sh`** 会将 **`.runtime/scripts`** 加入 `PATH`，便于放置自行下载的工具。
