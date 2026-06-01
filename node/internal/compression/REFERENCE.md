# compression 模块参考

| 符号 | 说明 |
|------|------|
| `Coordinator` | 压缩协调器（silent 异步 + blocking 同步） |
| `NewCoordinator(client, silentTokens, blockingTokens)` | 构造；阈值 `<=0` 关闭对应档位 |
| `Enabled()` | 是否启用（任一阈值 >0 且 client 非空） |
| `MaybeHandle(ctx, sessionID, agentID, hub, messages)` | 每条 message 入口：应用 pending、触发压缩；SSE `context_compression_blocking` / `context_compression_silent`（phase=start/end） |
| `CancelSession(sessionID)` | 取消 silent 任务并丢弃 pending |
| `shouldCompress` / `selectCompressRange` | 阈值判定与区间选择（plan.go） |
| `messagesFingerprint` | 区间指纹，防止 stale 应用 |
