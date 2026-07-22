# promptcontext

从 SQLite（`agents.db` → `agent_prompt_context`）经 `Content` 注入 system prompt；`EnsureAgentPromptContext` 仅在首次建 Agent 行时从旧版 `.runtime` 文件迁移。

| 文件 | 职责 |
|------|------|
| `reader.go` | `Reader`：读取 soul/user/custom/long_term、`UpdateLongTerm` |
| `content.go` | `Content`、`SetContent` |

长期记忆写入请使用 `remember` 工具（非工作区文件）。
