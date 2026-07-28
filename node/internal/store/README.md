# node/internal/store

Agent 对话运行时持久化（SQLite）。

## 职责

| 文件 | 说明 |
|------|------|
| `sqlite.go` | `SQLiteStore`：`agent_runtimes` 行读写、`messages_json`、`loaded_skills_json`；启动时将旧表 `sessions` 迁入 |
| `runtime_state.go` | `RuntimeState`：`pending` HITL、`tool_loop_count` 等恢复字段 |
| `sqlite_test.go` | 存储 round-trip 与 legacy 迁移单测 |

## Schema

| 列 | 含义 |
|----|------|
| `agent_id` | 对话 / Agent 实例 id（主键；旧 `session_id`） |
| `node_id` | 所属 Node 身份（旧列名曾误为 `agent_id`） |

默认文件路径仍为 `{runtime}/memory/sessions.db`（兼容既有部署）。

## 边界

- **热状态**在 `session/runtime` 内存；`persist` 时全量快照写入本包。
- **JSONL 审计**在 `history/Journal`，与本包并行、不替代。
- `RuntimeState` 嵌入 `turn.PendingHITL`，恢复后由 `session` 喂回 `turn` 续跑。
- Agent 元数据（模板/快照等）在 `agents.db`，与本库分离。

## 相关文档

- Session 持久化时机：[`../session/README.md`](../session/README.md)
- 架构：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
