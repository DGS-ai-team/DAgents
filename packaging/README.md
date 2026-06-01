# `packaging/`

分发辅助说明（非运行时模块），可随内网拷贝包附带给用户。

## 制品命名约定

| 名称 | 含义 |
|------|------|
| **`dagents-api`**（可执行文件） | 仅 Agent HTTP/SSE 服务进程（`run_agent_api.py`） |
| **`dagents-backend-*.{zip,tar.zip,deb,rpm,exe}`** | 完整后端运行时（api + register_center + cli + `.runtime` + 启动脚本）的便携包或安装包 |
| **`dagents_register_center` / `dagents-cli`** | 同 bundle 内的其它可执行文件，不单独出现在 zip/deb 文件名中 |

便携 zip/tar 与 deb/Windows 安装包统一使用 **`dagents-backend-*`** 前缀，避免与单个 **`dagents-api`** 二进制混淆。

| 路径 | 说明 |
|------|------|
| **`agent-client/`** | Go Node + Client **共用 YAML** 示例（`config.example.yaml`）；见 [agent-client/README.md](./agent-client/README.md) |
| **`OFFLINE_INSTALL.md`** | 离线安装步骤摘要；亦可复制到 README「离线依赖」章节 |
| **`runtime/`** | 预编译包内 **`.runtime/`** 整树占位：**`prompt_context/`**（空 **`soul.md`/`user.md`/`custom.md`**）、**`scripts/`**、**`data/`**、**`skills/`**、**`history/`**、**`memory/`**、**`agent/`** 等；打 zip 时并入 **`bundle/.runtime/`**；侧车无预设文案，运行时仍可由 **`prompt.py`** 补建缺失文件 |
| **`windows/`** | Windows 安装器资源：Inno Setup 脚本与 `dagents.cmd`（`pushd "%~dp0."`；子命令统一 `dagents-cli.exe %*`，勿 `shift` 后再拼固定子命令 + `%*`，否则会出现 `chat chat` 等重复参数） |
| **`linux/`** | Linux 安装包资源：`.deb` / `.rpm` 构建脚本、`dagents-backend.spec` 与安装后暴露的 `dagents` 命令入口（含 `chat` 交互终端） |
