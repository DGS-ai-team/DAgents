# Agent + Client 同包示例

Node 与 Client 共用 YAML 配置；**仓库只提交 `*.example.yaml`**，本地副本见根目录 `.gitignore`。

双 Client 说明见 [local-assistant.md](../../docs/architecture/local-assistant.md)。

## 文件

| 文件 | 说明 |
|------|------|
| `config.example.yaml` | 配置示例（可提交）；含 listen、local、llm、**fs_root**、**manage/a2a**、**triggers** 等 |
| `agent-card.example.json` | **被调方** Card 完整范例（`metadata.role=compliance`）→ 复制为 `agent-card.json` |
| `agent-card.example.ops.json` | **纯调用方** Card 范例（`role=ops` + `compliance_peer`） |
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
| `metadata.role` | **Card 专用** | Node **本地** Inbox 行为（见下表） |
| `metadata.compliance_peer` | **Card 专用** | `agent_invoke` 省略 `to_agent_id` 时的默认目标 |

### Agent Card 字段（`agent-card.json`）

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 建议 | Manage Console 展示名；空则回退 `agent_id` |
| `description` | 建议 | Console 简介 |
| `url` | 可选 | 写入 `card` blob 供展示；**A2A 路由不用**（用 `manage.registration.base_url`） |
| `version` | 可选 | 写入 `card` blob；注册顶层 `version` 来自 Node 二进制 |
| `capabilities` | 可选 | A2A 业务语义（如 `compliance_review`）；非空时**覆盖** config 默认 capabilities |
| `skills` | 可选 | 仅展示；会话 skill 由 `load_skills` 加载，不进 Card 扫描 |
| `defaultInputModes` | 可选 | 如 `["text"]` |
| `defaultOutputModes` | 可选 | 如 `["text"]` |
| `metadata.role` | A2A 被调建议 | `compliance` → Inbox 合规 turn；其它值当前无 handler |
| `metadata.compliance_peer` | 调用方建议 | `agent_invoke` 默认 `to_agent_id`（如 `"node-a"`） |
| `metadata.*` | 可选 | 自定义键（部门、咨询前缀等），原样进 `card` |

### `expose_to_peers`（config）与 `metadata.role`（Card）的区别

| | **`expose_to_peers`**（`config.yaml`） | **`metadata.role`**（`agent-card.json`） |
|---|----------------------------------------|------------------------------------------|
| **作用域** | Manage **信令层**：能否作为 A2A **目标**被投递 | Node **进程内**：收到 Inbox Task 后走哪条 handler |
| **谁读取** | Manage Registry / discover / create Task 校验 | Go Node `ResolveInboxHandler`、工具默认参数 |
| **典型值** | `true` = 可出现在 discover、可接收 Task；`false` = 仅注册运维可见 | `compliance` = 跑合规 turn 并 reply；`ops` = 无 Inbox handler |
| **与 A2A 关系** | **门卫**：关则 Manage 直接拒投（`403 target_not_exposed`） | **工人**：开门卫后，决定进门有没有人去干活 |
| **组合示例** | compliance 被调：`expose_to_peers: true` + `role: compliance` + `manage.a2a.enabled: true` | ops 调用方：`expose_to_peers: false`（或 true 但不收信）+ `role: ops` + `a2a.enabled: false` |

**易错**：`expose_to_peers: true` 但 `role` 不是 `compliance`（且无其它 handler）时，Task 会变为 `delivered` 并卡在日志 `a2a inbox task received (no handler)`。

Card 文件路径固定为 Node **启动时工作目录**下的 `agent-card.json`（不可通过 config 配置）。双 Node Docker 范例见 `cases/a2a-manage-docker/agent-card/`。

### A2A HITL 中继（调用方 Client）

当本机 `agent_invoke` 轮询到 callee 的 `requires_input` 时，Node 经 `A2ACallerHITLBridge` 向 **caller session** 推送带 `a2a_relay` 的 HITL SSE；Python / Go TUI 在工具行展示 **`from <对端 Agent>`**，审批提交后不等待本地 `tool_result`。详见 [manage-communication.md](../../docs/manage-communication.md) §4.2 与 [a2a-via-manage.md](../../docs/future/a2a-via-manage.md) §5.0。

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
