# REFERENCE — `node/internal/manage`

## `registrar.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `ToolNamesProvider` | `func() []string` | 心跳时刷新 tools 列表 |
| `Registrar` | `struct` | Manage 注册 sidecar |
| `NewRegistrar` | `func(cfg, logger) *Registrar` | 构造；需 `cfg.Manage.Enabled` |
| `(r *Registrar) SetToolNamesProvider` | `method` | 注入工具名（通常 `session.Manager.ToolNames`） |
| `(r *Registrar) Registered` | `method` | 最近一次 register/heartbeat 是否成功 |
| `(r *Registrar) Start` | `method` | 后台 goroutine；ctx 取消时退出 |
| `(r *Registrar) Stop` | `method` | deregister 并清除注册态 |

HTTP Header：`x-dagents-agent-id`（`agent_id`）；`x-dagents-a2a-token` 可选（Token 模式）。

## `agentcard.go`

| 符号 | 说明 |
|------|------|
| `AgentCard` | Agent Card JSON 模型 |
| `LoadAgentCard` | 从 `manage.registration.agent_card_path` 加载 |

## `compliance_executor.go`

| 符号 | 说明 |
|------|------|
| `ComplianceExecutor` | 合规 inbox：ack → `session.RunInboxConsultation`（流式 LLM turn）→ reply |
| `ResolveInboxHandler` | 按 Agent Card `metadata.role` 选择 handler；需 `*session.Manager` |

## `inbox_poller.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `InboxTask` | `struct` | Manage inbox 单条 Task |
| `InboxTaskHandler` | `func(ctx, InboxTask) error` | 收到 Task 回调（待接 session 入队） |
| `InboxPoller` | `struct` | long poll + 断线短 poll |
| `NewInboxPoller` | `func(cfg, logger) *InboxPoller` | 构造 |
| `(p *InboxPoller) SetHandler` | `method` | 注入 Task 处理 |
| `(p *InboxPoller) Start` | `method` | 后台 goroutine |

配置：`manage.a2a.enabled`（默认随 `manage.enabled` 开启）、`inbox_wait_seconds`、`inbox_poll_seconds`。
