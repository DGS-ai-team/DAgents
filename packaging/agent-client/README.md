# Agent + Client 同包示例

Node 与 Client 共用 YAML 配置；**仓库只提交 `*.example.yaml`**，本地副本见根目录 `.gitignore`。

双 Client 说明见 [local-assistant.md](../../docs/architecture/local-assistant.md)。

## 文件

| 文件 | 说明 |
|------|------|
| `config.example.yaml` | 配置示例（可提交）；含 listen、local、llm、data_dir、**triggers** 等 |
| `scripts/` | 分发包内启动脚本（`startup/`）说明；CI 另复制 `scripts/linux|windows|service` 系统服务注册脚本 |
| `policy.example.yaml` | **已废弃**（legacy YAML）；现用 `.runtime/policy/*.approval.txt` |

`config.example.yaml` 中 **`triggers`** 块：`enabled` 开关、`poll_seconds` 轮询间隔、可选 `store_path`；condition 语法见 [`node/internal/triggers/README.md`](../../node/internal/triggers/README.md)。

## 本地配置

```bash
# 在仓库根目录执行
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
```

编辑 `config.yaml`（例如 `llm.mock: false`）。工具审批策略在 **`.runtime/policy/`**（首次启动 Node 时从 `packaging/runtime/policy` 自动复制种子）。

`config.yaml` **不会被 Git 跟踪**。

## 注册为系统服务（Node）

解压包内（非开发仓库）：

```bash
sudo ./scripts/linux/install_node_service.sh install --config config.yaml
```

**Linux**（开发仓库，需 root）：

```bash
sudo scripts/linux/install_node_service.sh install --config packaging/agent-client/config.yaml --build
scripts/linux/install_node_service.sh status
sudo scripts/linux/install_node_service.sh uninstall
```

**Windows**（管理员 CMD）：

```bat
scripts\windows\install_node_service.cmd install packaging\agent-client\config.yaml --build
scripts\windows\install_node_service.cmd status
scripts\windows\install_node_service.cmd uninstall
```

Linux 注册 **systemd**（`dagents-node.service`）；Windows 为 **SYSTEM 开机计划任务**（非 Services.msc）。详见 [`scripts/README.md`](../../scripts/README.md)。

## 快速联调（一份配置，三个进程共用）

在仓库根目录复制一份本地配置（只需一次）：

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
# 编辑 config.yaml；策略文件在首次启动 Node 后出现在 .runtime/policy/
```

启动 Node：

```bash
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
```

Python TUI Client：

```bash
python -m app.cli.main chat --config packaging/agent-client/config.yaml
```

详见 [docs/architecture/local-assistant.md](../../docs/architecture/local-assistant.md)。
