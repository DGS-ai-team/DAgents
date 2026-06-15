# node/internal/toolresult

WS3：tool 结果写入 history 前的**落盘 + 头尾摘要**（首版仅 `bash_run` 经 `hooks.tool_result` 启用）。

- **`Package`**：超长则写入 `fs_root/<spill_subdir>/<session>/<tool_call_id>.txt`，history 返回头 + `...（已省略约 N tokens）...` + 尾 + `read_file` hint。长度按 [DeepSeek token 粗算](https://api-docs.deepseek.com/zh-cn/quick_start/token_usage)（汉字×0.6、其它×0.3）。
- **Client/SSE** 使用 Hook 输出的 `ForClient`（全文，清洗后）。

配置见 `hooks.tool_result`（`shared/config`）与设计文档 [tool-context-cost-analysis.md §3.2.1](../../../docs/design/tool-context-cost-analysis.md)。
