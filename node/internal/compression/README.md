# node/internal/compression

Go Node 侧上下文摘要压缩：silent 异步 + blocking 同步，行为对齐 Python `SummaryCompressionCoordinator`。

| 文件 | 说明 |
|------|------|
| `coordinator.go` | `Coordinator`：MaybeHandle、silent goroutine、pending 应用 |
| `plan.go` | token 估算、区间选择、文本块格式化 |
| `fingerprint.go` | 消息快照与区间指纹（防 stale 应用） |

配置见 `shared/config.Config.Compression`：

- `silent_trigger_tokens`：`<=0` 关闭 silent
- `blocking_trigger_tokens`：`<=0` 关闭 blocking

阻塞优先于静默；两者均通过 pending + 指纹校验后替换为单条 `user` 摘要消息。
