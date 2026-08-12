# REFERENCE — `client/`

精简 Client 模块（Phase 4 后）：

| 路径 | 说明 |
|------|------|
| `cmd/dagents-client` | `probe` / `update` / `version`；`chat`/`tui` 返回退出码 2 |
| `internal/api` | Node HTTP 客户端（更新流复用） |
| `internal/probe` | `/health` 探活 |
| `internal/update` | Manage Release Hub 下载 |
| `internal/desktop` | 桌面更新辅助 |

人机交互见 `node/webui/`。
