# node/internal/store

Session 持久化（SQLite）。

## 职责

| 文件 | 说明 |
|------|------|
| `sqlite.go` | `SQLiteStore`：session 行读写、`messages_json`、`loaded_skills_json` |
| `runtime_state.go` | `RuntimeState`：`pending` HITL、`tool_loop_count` 等恢复字段 |
| `sqlite_test.go` | 存储 round-trip 单测 |

## 边界

- **热状态**在 `session/runtime` 内存；`persist` 时全量快照写入本包。
- **JSONL 审计**在 `history/Journal`，与本包并行、不替代。
- `RuntimeState` 嵌入 `turn.PendingHITL`，恢复后由 `session` 喂回 `turn` 续跑。

## 相关文档

- Session 持久化时机：[`../session/README.md`](../session/README.md)
- 架构：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
