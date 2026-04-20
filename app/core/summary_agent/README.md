# `app/core/summary_agent/`

上下文压缩 Agent：外层传入阈值后先 `should_compress` 判定并记录区间，再 `prepare_compression_block` 构建文本块，`run_turn` 生成摘要并写入 context，最后可按区间替换消息。

| 文件 | 说明 |
|------|------|
| **`agent.py`** | **`SummaryContextCompressionRuntime`**（`should_compress` / `prepare_compression_block` / `run_turn` / `replace_messages_with_compressed_message`）与 **`init_agent()`** |
