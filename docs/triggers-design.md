# 触发器（条件唤起 Agent）

> **现网**：Go Node `node/internal/triggers/`。符号与 condition 语义见 [node/internal/triggers/README.md](../../node/internal/triggers/README.md)。

## 能力概要

- **condition 类型**（三选一）：`interval_seconds`、`fire_at`、`schedule`（daily / weekly / monthly）；可选 `cmd` 门控（仅 schedule 自动触发）。
- **投递**：调度器 `FireTrigger` → session 队列，与用户 `POST` 消息同构 turn 路径。
- **工具**：`trigger_list` / `trigger_create` / `trigger_update` / `trigger_delete`（无 `trigger_fire`，触发靠调度或 HTTP API）。
- **HTTP**：`GET/POST /v1/triggers` 等（见 [agent-node-api.md](./architecture/agent-node-api.md)）。
- **配置**：`triggers.enabled`、`triggers.poll_seconds`；存储 `{fs_root}/triggers/triggers.json`。

## 延伸阅读

- [handbook/04-能力与策略.md](./handbook/04-能力与策略.md) — triggers 与 policy 交互
- [handbook/附录/内置工具参考.md](./handbook/附录/内置工具参考.md) — trigger 工具字段
