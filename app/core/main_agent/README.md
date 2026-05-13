# `app/core/main_agent/`

主 Agent 运行时：OpenAI 隐式 ReAct 编排、模型客户端与系统提示词。

| 文件 | 说明 |
|------|------|
| **`agent.py`** | **`init_agent()`**（创建 OpenAI runtime）、**`MainAgentTurnOrchestrator`**（消息分支与工具审批/执行编排） |
| **`runtime_openai.py`** | OpenAI 原生 tool calling 运行时（会话状态、工具调用、审批中断、事件产出） |
| **`model.py`** | **`get_openai_client()`**、**`get_model_config()`** |
| **`prompt.py`** | **`get_system_prompt`**（侧车 **`.runtime/prompt_context/*.md`** + **`get_host_snapshot()`** 运行环境含 OS/用户）、**`get_static_system_prompt`** 等 |

高层行为与编排细节见本目录 **`README.md`**、**`REFERENCE.md`** 及源码；对外 HTTP/SSE 契约见 **`../../../doc/api-reference.md`**。
