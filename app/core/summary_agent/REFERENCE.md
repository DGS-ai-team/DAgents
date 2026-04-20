# `app/core/summary_agent/REFERENCE.md`

## `agent.py`

- **类**
  - **`SummaryContextCompressionRuntime`**：上下文压缩 runtime；`should_compress(ctx, trigger_tokens=...)` 按外层传入阈值判断并记录压缩区间；`estimate_message_tokens()` 提供粗略 token 估算；`prepare_compression_block()` 按区间格式化文本块；`run_turn()` 仅消费预格式化文本块并生成摘要并写入 `ctx.summary_compressed_message`；`replace_messages_with_compressed_message()` 按区间替换原消息。

- **函数**
  - **`init_agent()`**：创建 `SummaryContextCompressionRuntime` 实例。
