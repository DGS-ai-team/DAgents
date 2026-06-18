# node/internal/compression

Go Node 侧上下文摘要压缩：silent 异步 + blocking 同步；M2 侧车 `StreamChat` 与主 turn 前缀对齐（`BuildSystemPrompt` + `tools` + messages 前缀）。

| 文件 | 说明 |
|------|------|
| `coordinator.go` | `Coordinator`：`MaybeHandle` / `ForceBlocking`、silent goroutine、`readyCompressions` 写回 |
| `sidecar.go` | `SidecarPrefix`、`BuildSidecarChatRequest`、`Summarize`（StreamChat 摘要） |
| `plan.go` | `evaluateCompression`、`buildCompressionPlan`、`isSelectableCompressEnd`、两种 apply/sidecar 策略 |
| `apply.go` | `applyCompressionReplacement` 写回 messages |
| `metrics.go` | 压缩成功 SSE/API 的 DeepSeek `usage` 字段 |
| `fingerprint.go` | 消息快照与区间指纹（防 stale 应用） |

## 调用链

`runtime.runTurnStep` → `sidecarPrefix()` → `MaybeHandle(..., prefix)` →（阈值满足）`Summarize` → `applyReadyCompression`。

`SidecarPrefix` 由 `runtime` 注入（`SystemPromptForSession` + `ToolDefinitions`），compression 包不 import `turn`。

## 配置

`shared/config.Config.Compression`：

- `silent_trigger_tokens`：`<=0` 关闭 silent
- `blocking_trigger_tokens`：`<=0` 关闭 blocking

阻塞优先于静默。silent 在 `readyCompressions` pending 或 apply 后冷却期（默认 60s / +4000 tokens）内不重复启动侧车。

**History 与 system（P9）**：session/SQLite `messages` 仅含 user/assistant/tool；`BuildSystemPrompt` 经 `llm.MessagesWithSystem` 出站注入，不落库。压缩区间自 `leadingSystemSkip` 起（生产恒为 0）；journal 异常写入 leading `system` 时跳过以免与侧车 `SystemPrompt` 重复。

`evaluateCompression`：达阈值但 plan 失败时 `Should=false` 且 `TriggerLevel` 仍为 silent/blocking（P8）。

写回后经 fingerprint 校验；成功时 SSE `context_compression_*` 含 `prompt_tokens`、`completion_tokens`、cache hit/miss。最近一次成功压缩写入 `LastCompression`，经 `GET /context` 的 `last_compression` 暴露，并打结构化日志。

```bash
go test ./node/internal/compression/...
```

## 延伸阅读

- 可读摘要（背景 / 思路 / 落地）：[docs/design/major-changes.md](../../../docs/design/major-changes.md#1-上下文压缩与-prompt-cache-对齐m2--m3)
- 完整技术分析：[docs/design/context-compression-cache-analysis.md](../../../docs/design/context-compression-cache-analysis.md)
