# promptcontext

从 SQLite（`agents.db` → `agent_prompt_context`）经 `Content` 注入当前模型请求的运行时上下文；`EnsureAgentPromptContext` 仅在首次建 Agent 行时从旧版 `.runtime` 文件迁移。它不再拼接到稳定 system prompt。

| 文件 | 职责 |
|------|------|
| `reader.go` | `Reader`：读取 soul/custom/long_term，并注入 `PreferredName`、`UpdateLongTerm` |
| `content.go` | `Content`、`SetContent` |

**长期记忆加载时机**（`Orchestrator.ReloadLongTermMemory`）：

1. 清空上下文后
2. Agent 本段对话的首条用户消息（messages 为空时）
3. 上下文压缩成功写回后

`remember` 工具写入 DB 后仅 `UpdateLongTerm` 更新当前 session 内存，不触发 DB 重载。
