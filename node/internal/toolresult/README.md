# node/internal/toolresult

WS3：tool 结果写入 history 前的**落盘 + 头尾摘要**（默认 bash + terminal + fs，经 `hooks.tool_result.tools` 启用）。

- **`Package`**：单条结果 token 估算超过 `spill_threshold_tokens`（默认 12000）时，写入 Agent workspace 的 `tool_outputs/<agent_id>/<session>/<tool_call_id>.txt`（目录固定，不可配置）；history 返回头尾摘要 + `read_file` hint。阈值作用于 `tools` 列表中的**每个**工具，非 bash 专用，也非整段 session history 上限。
- **Client/SSE** 使用 Hook 输出的 `ForClient`（全文，清洗后）。

fs 工具另有工具内单页上限（如 read/grep 3000 tokens），与 Hook 阈值分层，见设计文档 §3.2.2。
