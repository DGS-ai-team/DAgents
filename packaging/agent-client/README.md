# Agent + Client 同包示例

Node 与 Client 共用 YAML 配置；**仓库只提交 `*.example.yaml`**，本地副本见根目录 `.gitignore`。

## 文件

| 文件 | 说明 |
|------|------|
| `config.example.yaml` | 配置示例（可提交）；含 `agent`、`listen`、`llm`、**fs_root**、**manage/a2a**、**triggers** 等 |

`config.example.yaml` 中 **`triggers`** 块：`enabled` 开关、`poll_seconds` 轮询间隔；condition 语法见 [`node/internal/triggers/README.md`](../../node/internal/triggers/README.md)。

**`tools.enabled_groups`**：按工具组配置 LLM 可见内置工具（省略=全部）；7 组定义见 [handbook/04-能力与策略.md](../../docs/handbook/04-能力与策略.md) §1 与 [`shared/config/README.md`](../../shared/config/README.md)。

## 本地配置

```bash
# 在仓库根目录执行
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
```

编辑 `config.yaml`（例如 `llm.mock: false`、Manage 场景下 `agent.role`）。工具审批策略在 **`.runtime/policy/`**（首次启动 Node 时从 `packaging/runtime/policy` 自动复制种子）。

`config.yaml` **不会被 Git 跟踪**。

### `agent` 块（身份与 A2A 角色）

所有 Agent 身份与 A2A 行为均在 **`config.yaml` 的 `agent` 块**配置；Node 注册 Manage 时自动组装 `card` JSON 上报。

| 字段 | 说明 |
|------|------|
| `name` | Manage 展示名；空则回退 `agent_id` |
| `description` | Console 简介 |
| `role` | **`compliance`** = 被调方（自动 expose + inbox + 合规 handler）；**`ops`** 等 = 调用方 |
| `compliance_peer` | 调用方：`agent_invoke` 省略 `to_agent_id` 时的默认目标 |
| `capabilities` | 可选；非空时覆盖 config 默认 capabilities（shell/filesystem 等） |
| `metadata` | 可选自定义键，原样进入 Manage `card.metadata` |

| `agent.role` | Manage expose | Inbox 轮询 | Handler |
|--------------|---------------|------------|---------|
| **`compliance`** | 是 | 是 | 合规 turn |
| **`ops`** 或其它 | 否 | 否 | 无 |

Manage 注册时仍合并 **`manage.registration`**（`base_url`、`team`、心跳参数）与运行时上报的 `tools` 列表。`discovery_group` 由 Manage Console/API 分配，不在 Node 配置。

双 Node Docker 范例见 `cases/a2a-manage-docker/config/node-{a,b}.yaml`。

### A2A HITL 中继（调用方 Client）

当本机 `agent_invoke` 轮询到 callee 的 `requires_input` 时，Node 经 `A2ACallerHITLBridge` 向 **caller session** 推送带 `a2a_relay` 的 HITL SSE。详见 [manage-communication.md](../../docs/manage-communication.md) §4.2。

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

Linux 注册 **systemd**（`dagents-node.service`）；Windows 为 **SYSTEM 开机计划任务**。详见 [`scripts/README.md`](../../scripts/README.md)。

## 快速联调（一份配置，三个进程共用）

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml
```

Manage + 双 Node A2A 见 [`cases/a2a-manage-docker/README.md`](../../cases/a2a-manage-docker/README.md)。
