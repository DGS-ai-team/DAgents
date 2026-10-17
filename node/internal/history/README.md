# `node/internal/history`

提供原始消息 JSONL 审计侧车，以及空闲后新对话使用的精简段归档。

**落盘路径**：`<workspace_root>/.dagents/<agent_id>/history/YYYYMMDD/<session_id>.jsonl`（按自然日分子目录；文件名经 sanitize；每个 Agent 在共享 workspace 中独立命名空间）。

Node 的 `memory/sessions.db` 仍是会话快照与恢复的控制面存储；上述文件不替代 SQLite。

| 文件 | 说明 |
|------|------|
| `journal.go` | `Journal`：按条追加 JSONL、`AppendMessage` / `InsertMessage`；调用方须只传入 canonical message，并先经 `llm.Client.NormalizeAssistant` |
| `context_archive.go` | `ArchiveIdleContext`：将权威活动段快照投影为只有 role/content 的 JSON 数组；工具名称、参数、结果和状态保留在 content 中；原子发布并校验同一 reset ID 的重试 |
| `normalize.go` | JSONL `message` 快照（委托 `llm.MessageToJournalPayload`） |
| `journal_test.go` | 开关、JSONL 行结构 |

**reasoning_content 存储规范化**已迁至 **`node/internal/llm/`**（按 `llm.provider` 的 `MessageAdapter`）。

Journal 是 append-only 审计侧车，不是会话恢复真相源，也不负责判断或修复工具协议。取消时的
流式 assistant/tool-call draft 不写入 canonical history，因而也不应写入 Journal；只有已经
提交到 `runtime.messages` 的完整消息才由上层追加。JSONL 只读作审计记录，不参与会话恢复，
也不会被原地迁移、删除或回写。

段归档写入 `<workspace_root>/.dagents/<agent_id>/history/YYYY-MM-DD/<bounded-name>.json`，实际路径由写入函数返回，跨日期重试保持最初的路径。它不依赖 raw journal 开关，写入失败向调用方返回错误，由 session 保留原活动上下文。推理内容、内部标识和统计字段不进入该模型可读投影；图片内联数据只保留可读占位，文件路径保留引用。

原子发布使用同目录临时文件和硬链接。工作区文件系统不支持硬链接时归档失败，新段切换安全跳过，日志记录原因。
