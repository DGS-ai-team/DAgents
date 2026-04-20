# `app/harness/service/`

独立 Agent 服务（常驻进程，通常由 FastAPI 层托管）：

- 持续读取消息队列
- 调用 OpenAI 隐式 ReAct runtime 处理消息
- 输出处理结果（当前先 stdout）

| 文件 | 说明 |
|------|------|
| **`agent_service.py`** | `AgentService`：生命周期、会话（队列 + **`OpenAIConversationContext`** 缓存 + sqlite）、调用 **`run_turn(ctx, ...)`** 与流式输出；在服务层处理工具编排（`tool_call -> approval_required`、`resume approve/reject`、`async_tool_result` 回灌）；**`submit_message`** 仅入队（**`human`** 只影响优先级），是否打断在途 turn 由客户端调 **`cancel_current_turn`** |
| **`interface.py`** | `AgentSubmitRequest/Result`、`AgentEventEnvelope`、`AgentStreamEventData` 与 `AgentServiceClient` 协议（统一接口抽象） |
| **`http_client.py`** | `HttpAgentServiceClient`：通过 FastAPI（`/v1/sessions`、`/v1/messages` + SSE）调用 `agent_service` |

