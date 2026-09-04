# node/internal/store

Agent 对话运行时持久化（SQLite）。

## 职责

| 文件 | 说明 |
|------|------|
| `sqlite.go` | `SQLiteStore`：`agent_runtimes` 行读写、`messages_json`、`loaded_skills_json`；启动时将旧表 `sessions` 迁入 |
| `runtime_state.go` | `RuntimeState`：旧版 `pending` / `tool_loop_count` 兼容镜像，以及 Hook/cursor 字段 |
| `sqlite_test.go` | 存储 round-trip 与 legacy 迁移单测 |

## Schema

| 列 | 含义 |
|----|------|
| `agent_id` | 对话 / Agent 实例 id（主键；旧 `session_id`） |
| `node_id` | 所属 Node 身份（旧列名曾误为 `agent_id`） |

默认文件路径仍为 `{runtime}/memory/sessions.db`（兼容既有部署）。该库属于 Node 控制面，
不随 Agent workspace 移动；workspace 可能被多个 Agent 共享，Agent 记录通过 `agent_id` 隔离。

## 边界

- **生命周期热状态**在 `turn.Coordinator` 内存；`persist` 时消息与兼容性镜像写入本包。
- **JSONL 审计**在当前 Agent workspace 的 `.dagents/<agent_id>/history/`，与本包并行、不替代。
- `turn_events` 是 Turn/Step 生命周期事实源；`RuntimeState.Pending` / `ToolLoopCount` 只用于无事件老数据迁移及旧读取方兼容，不作为新的执行位置。
- Agent 元数据（模板/快照等）在 `agents.db`，与本库分离。

## 相关文档

- Session 持久化时机：[`../session/README.md`](../session/README.md)
- 架构：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
