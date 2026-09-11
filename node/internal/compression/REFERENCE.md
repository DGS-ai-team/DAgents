# compression 模块参考

| 符号 | 说明 |
|------|------|
| `Coordinator` | 压缩协调器（silent 异步 + blocking 同步） |
| `NewCoordinator(client, silentTokens, blockingTokens)` | 构造；阈值 `<=0` 关闭对应档位 |
| `Enabled()` | 是否启用（任一阈值 >0 且 client 非空） |
| `MaybeHandle(ctx, sessionID, agentID, hub, messages, prefix)` | 每条 message 入口：写回 ready、触发压缩；`prefix` 与主 turn 同源 |
| `CancelSession(sessionID)` | 取消 silent 任务并丢弃 pending |
| `evaluateCompression` | 单次阈值 + plan；P8：`TriggerLevel` 在达阈值不可压时仍保留 silent/blocking |
| `leadingSystemSkip` | P9：跳过 leading `system`（生产路径恒为 0） |
| `computePrefixClosure` / `isSelectableCompressEnd` | 轮次状态机 + 可选边界；非法 messages 序列不压缩、不修复 |
| `SidecarPrefix` / `BuildSidecarChatRequest` / `Summarize` | 侧车 StreamChat 前缀对齐摘要；尾部 user `name=compression_sidecar`（不落库） |
| `FinalizeCompressionSummary` | LLM 摘要写回前追加当前 workspace `.dagents/<agent_id>/history/…` 审计位置说明（需 `SetRawMessageHistoryEnabled(true)`；无 workspace 前缀时兼容 `<runtime_root>/history/…`） |
| `attachCompressionUsageMetrics` | 压缩成功 SSE/API 的 DeepSeek usage 字段（metrics.go） |
| `LastCompression` / `LastCompressionSnapshot` | 最近一次成功写回压缩的 usage 快照；`GET /context` 与日志 |
| `shouldStartSilent` / `SilentCooldownDuration` | silent 冷却与 pending 去重（`silent_cooldown.go`） |
| `applyCompressionReplacement` | 按 `compressApplyMode` 写回 messages（user `name=compression`） |
| `messagesFingerprint` | 区间指纹，防止 stale 应用 |
| `ForceBlocking` | 手动阻塞压缩（POST `/compress`）；`applied` 时含 DeepSeek usage 字段 |
| `SetCandidateSubmitter` | 可选压缩候选扩展；把 frozen slice 以有界、非阻塞提交交给 memory pipeline，不进入 Turn/MessageQueue；未绑定时完全关闭 |

压缩成功 SSE/API 字段对齐 [DeepSeek Chat Completions usage](https://api-docs.deepseek.com/zh-cn/api/create-chat-completion)：`prompt_tokens`（输入）、`completion_tokens`（摘要输出）、`prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`。设计背景见 [`docs/design/context-compression-cache-analysis.md`](../../../docs/design/context-compression-cache-analysis.md)。
