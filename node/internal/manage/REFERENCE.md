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

HTTP Header：`x-dagents-agent-id`（`agent_id`）；`x-dagents-a2a-token` 可选（Token 认证命名）。

## `registration_card.go`

| 符号 | 说明 |
|------|------|
| `RegistrationCard` | 从 `config.yaml` `agent` 块组装 Manage 注册 card |

## 已拆除（2026-08）

以下符号/文件已删除：`inbox_poller.go`、`compliance_executor.go`、`task_replier.go`、`a2a_profile.go`（`LogA2AProfileWarnings`），以及 Node `agent_invoke` / `agent_discover` 工具。跨机协作请用工作组 Dialer。
