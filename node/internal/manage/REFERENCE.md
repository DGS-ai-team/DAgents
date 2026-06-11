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
