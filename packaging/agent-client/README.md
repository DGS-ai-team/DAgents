# Agent + Client 同包示例

Node 与 Client 共用 YAML 配置；**仓库只提交 `*.example.yaml`**，本地副本见根目录 `.gitignore`。

双 Client 说明见 [local-assistant.md](../../docs/architecture/local-assistant.md)。

## 文件

| 文件 | 说明 |
|------|------|
| `config.example.yaml` | 配置示例（可提交）；含 listen、local、llm、**fs_root**、**manage/a2a**、**triggers** 等 |
| `agent-card.example.json` | Manage 注册 **Card 侧车**（固定复制为工作目录下 **`agent-card.json`**） |
| `scripts/` | 分发包内启动脚本（`startup/`）说明；CI 另复制 `scripts/linux|windows|service` 系统服务注册脚本 |
| `policy.example.yaml` | **已废弃**（legacy YAML）；现用 `.runtime/policy/*.approval.txt` |

`config.example.yaml` 中 **`triggers`** 块：`enabled` 开关、`poll_seconds` 轮询间隔；condition 语法见 [`node/internal/triggers/README.md`](../../node/internal/triggers/README.md)。

**`tools.enabled_groups`**：按工具组配置 LLM 可见内置工具（省略=全部）；7 组定义见 [`docs/built-in-tools.md`](../../docs/built-in-tools.md) §0 与 [`shared/config/README.md`](../../shared/config/README.md)。

## 本地配置

```bash
# 在仓库根目录执行
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
```

编辑 `config.yaml`（例如 `llm.mock: false`）。工具审批策略在 **`.runtime/policy/`**（首次启动 Node 时从 `packaging/runtime/policy` 自动复制种子）。

`config.yaml` **不会被 Git 跟踪**。

### Agent Card 与 `config.yaml` 分工

注册 Manage 时，Node 在 `buildRegisterPayload`（`node/internal/manage/registrar.go`）中合并两路配置：

| 字段 | 来源 | 说明 |
|------|------|------|
| `base_url` | **`manage.registration.base_url`**（空则 `local.endpoint`） | **不要**在 Card 里写 `url` |
| `team` | **`manage.registration.team`** | **不要**写在 Card `metadata.team` |
| `name` / `description` | **`agent-card.json` 唯一来源**（`name` 空则 `agent_id`） | **不要**在 `config.yaml` 的 registration 重复填写 |
| `version` | **Node 二进制版本** | Card 里 `version` 仅原样进 `card`  blob，不参与顶层 |
| `expose_to_peers` | **`expose_to_peers`** | config 控制 |
| `tools` | **心跳自动上报**（当前 registry 工具名） | 勿手填 |
| `capabilities`（顶层） | 默认 **`config.Capabilities()`**（shell/filesystem/triggers）；Card 非空则 **覆盖** | Card 宜写 **A2A 业务语义**（如 `compliance_review`），勿写 filesystem/shell |
| `skills` | **当前未自动上报**；Card 手填也不会从 `.runtime/skills` 扫描 | 会话内 skill 由 **`load_skills`** 加载，不进 Card |
| `metadata.role` | **Card 专用** | `compliance` 时启用 Inbox 合规 handler |
| `metadata.compliance_peer` | **Card 专用** | `agent_invoke` 默认对端 agent_id |

Card 文件路径固定为 Node **启动时工作目录**下的 `agent-card.json`（不可通过 config 配置）。最小模板见 `agent-card.example.json`；双 Node 完整示例见 `cases/a2a-manage-docker/agent-card/`。

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
