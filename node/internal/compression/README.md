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

可选的记忆候选提取挂在压缩完成边界：`runCompressionFlow` 先把本次压缩区间冻结为
`memory.ExtractionInput`，通过 `SetCandidateSubmitter` 非阻塞地提交到 Agent runtime
的有界单 worker 管线。压缩流程不等待提取、不会向消息队列追加 human/tool 消息，也不会
绕过记忆审批；默认 `memory.auto_extract=false`，只有显式开启后才会调用 LLM 提取候选。
候选的 `SourceFingerprint` 用于追溯来源，runtime 只发送 `memory/changed` 元数据事件，
前端或下一轮由正常的记忆快照边界读取结果。

## 配置

`shared/config.Config.Compression`：

- `silent_trigger_tokens`：`<=0` 关闭 silent
- `blocking_trigger_tokens`：`<=0` 关闭 blocking
- `idle_new_conversation_seconds`：普通 Agent 空闲后，下一条真实用户输入到来时开启新的模型上下文段；`0` 关闭。该功能由 session 包处理，不调用摘要模型。

阻塞优先于静默。silent 在 `readyCompressions` pending 或 apply 后冷却期（默认 60s / +4000 tokens）内不重复启动侧车。

**空闲后的新对话**：使用持久化的交互完成时间判断空闲，先归档上一段，再推进活动上下文起点；保留完整 SQLite 历史。Auto、审批恢复和活动轮次不因用户空闲被打断。后台不再恢复闲置 runtime 进行 LLM 压缩，活动中的 token 阈值压缩仍保留。详见[设计与验收](../../../docs/design/idle-conversation-boundary.md)。

**History 与 system（P9）**：session/SQLite `messages` 仅含 user/assistant/tool；`BuildSystemPrompt` 经 `llm.MessagesWithSystem` 出站注入，不落库。压缩区间自 `leadingSystemSkip` 起：跳过防御性的 leading `system`，并保留紧随其后的空闲新对话 bridge，避免摘要替换掉持久化分隔线。其他 runtime notice 和技能消息不会因此全部固定。

`evaluateCompression`：达阈值但 plan 失败时 `Should=false` 且 `TriggerLevel` 仍为 silent/blocking（P8）。

写回后经 fingerprint 校验；成功时 SSE `context_compression_*` 含 `prompt_tokens`、`completion_tokens`、cache hit/miss。最近一次成功压缩写入 `LastCompression`，经 `GET /context` 的 `last_compression` 暴露，并打结构化日志。

```bash
go test ./node/internal/compression/...
```

## 延伸阅读

- 可读摘要（背景 / 思路 / 落地）：[docs/handbook/附录/重大设计变更实录.md](../../../docs/handbook/附录/重大设计变更实录.md#1-上下文压缩与-prompt-cache-对齐m2--m3)
- 完整技术分析：[docs/design/context-compression-cache-analysis.md](../../../docs/design/context-compression-cache-analysis.md)
