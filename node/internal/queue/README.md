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
continuation(-1) > resume(1) > async_completion(2)
```

| 标签 | 典型 request_type | 说明 |
|------|-------------------|------|
| `continuation` | `turn_continuation` / `side_effect_continue` | 恢复或旁路 Apply 后续跑 LLM |
| `resume` | `resume` | HITL 提交 |
| `async_completion` | `async_tool_result` | 浏览器任务 Produce（缓冲 + SSE，不 inline Apply） |

**边界**：队列不含 consumer；`session.runtime.consumeLoop` 负责 `Dequeue` 并分发 handler。trigger/child-agent/user 进入 InputBox FIFO；async 工具完成先 Produce 入缓冲，再在 `runTurnStep` 步首 Apply。pending HITL 不会被普通输入打断，只有显式 cancel 才结束当前 Turn。

## 相关文档

- Session 消费循环：[`../session/README.md`](../session/README.md)
- 架构图：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
