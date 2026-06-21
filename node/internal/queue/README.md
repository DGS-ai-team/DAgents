# node/internal/queue

Per-session 进程内优先级消息队列（对齐 Python `MessageQueue` 语义子集）。

## 职责

| 文件 | 说明 |
|------|------|
| `queue.go` | `MessageQueue`：入队、阻塞出队、优先级排序 |
| `envelope.go` | `Envelope`、`AsyncToolResultPayload`、request type 常量 |
| `queue_test.go` | 优先级与解析单测 |

## 优先级（高 → 低）

与 `queue.go` → `priorityValue` 一致（数值越小越先出队；同档 FIFO）：

```text
side_effect_continue(-1) > tool_result(-1) > human(0) > resume(1) > async_completion(2) > other(10)
```

| 标签 | 典型 request_type | 说明 |
|------|-------------------|------|
| `side_effect_continue` | `side_effect_continue` | 旁路缓冲 Apply 后续跑 LLM（与 `tool_result` 同优先级 -1） |
| `tool_result` | `tool_result` | 同步工具批闭合续跑 |
| `human` | `message` | 用户/子任务 human 抢占 |
| `resume` | `resume` | HITL 提交 |
| `async_completion` | `async_tool_result` | 后台 job Produce（缓冲 + SSE，不 inline Apply） |
| `other` | `trigger_message` / `a2a_inbox_message` | trigger / A2A inbox Produce |

**边界**：队列不含 consumer；`session.runtime.consumeLoop` 负责 `Dequeue` 并分发 handler。async/trigger/a2a **Produce** 入缓冲；**Apply** 在 `runTurnStep` 步首。pending HITL **不**阻塞出队（Issue #32）。

## 相关文档

- Session 消费循环：[`../session/README.md`](../session/README.md)
- 架构图：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
