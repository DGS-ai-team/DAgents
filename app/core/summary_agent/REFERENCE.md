# `app/core/summary_agent/REFERENCE.md`

## `agent.py`

- **类**
  - **`SummaryContextCompressionRuntime`**：上下文压缩 runtime；`should_compress(messages, silent_trigger_tokens=..., blocking_trigger_tokens=..., messages_total_tokens=...)` 基于 context 传入的总 token 判断阈值并返回触发层级（`none/silent/blocking`）；`build_compression_plan(messages)` 选择区间并格式化文本块；`build_follow_content(messages, end=...)` 构造压缩区间后的后续文本块；`run_turn(content=..., follow_content=...)` 会将两段内容拼接后生成摘要文本。

- **函数**
  - **`init_agent()`**：创建 `SummaryContextCompressionRuntime` 实例。
