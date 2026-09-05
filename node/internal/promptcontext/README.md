# promptcontext

从 SQLite（`agents.db` → `agent_prompt_context`）经 `Content` 注入当前模型请求的运行时上下文；它不拼接到稳定 system prompt。

| 文件 | 职责 |
|------|------|
| `reader.go` | `Reader`：读取 soul/custom，并注入 `PreferredName` |
| `content.go` | `Content`、`SetContent` |

记忆不再属于 prompt sidecar。Memory service 在新的模型上下文边界按
`MemoryAutoRecall` 执行请求级召回，并跟随当前用户消息注入；`remember`
工具写入的是 workspace memory store，不会改写 `Content`。
