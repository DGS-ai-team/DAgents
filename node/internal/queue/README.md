# node/internal/queue

Per-session 进程内优先级消息队列（对齐 Python `MessageQueue` 语义子集）。

## 职责

| 文件 | 说明 |
|------|------|
| `queue.go` | `MessageQueue`：入队、阻塞出队、优先级排序 |
| `envelope.go` | `Envelope`、`AsyncToolResultPayload`、request type 常量 |
| `queue_test.go` | 优先级与解析单测 |

## 优先级（高 → 低）

1. `tool_result` — 工具批执行后续跑
2. `human` — 用户消息
3. `resume` — HITL 恢复
4. `async_completion` — 异步工具完成回灌
5. `other` — trigger 等

**边界**：队列不含 consumer；`session.runtime.consumeLoop` 负责 `Dequeue` 并分发 handler。

## 相关文档

- Session 消费循环：[`../session/README.md`](../session/README.md)
- 架构图：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
