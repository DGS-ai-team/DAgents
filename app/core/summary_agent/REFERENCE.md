# `app/core/summary_agent/REFERENCE.md`

## `agent.py`

- **类**
  - **`SummaryContextCompressionRuntime`**：上下文压缩 runtime；`should_compress(messages, silent_trigger_tokens=..., blocking_trigger_tokens=...)` 统一判断阈值并返回触发层级（`none/silent/blocking`）；`build_compression_plan(messages)` 选择区间并格式化文本块；`run_turn()` 仅消费预格式化文本块并返回摘要文本；`estimate_message_tokens()` 提供粗略 token 估算。

- **函数**
  - **`init_agent()`**：创建 `SummaryContextCompressionRuntime` 实例。
