# `app/core/main_agent/` REFERENCE

## `agent.py`

- **`init_agent`**：创建并返回 OpenAI 隐式 ReAct runtime
- **`MainAgentTurnOrchestrator`**：消息回合业务编排器；`resume` / `async_tool_result` / `tool_result` / `human_message` 分支；`run_turn` 与工具审批/执行、tool_result 回灌；并内聚 summary 压缩入口流程（已完成结果替换、阻塞压缩、静默压缩任务管理）

## `runtime_openai.py`

- **`OpenAIImplicitReActRuntime`**：OpenAI 原生 tool calling 运行时（仅推理：**`run_turn(ctx: OpenAIConversationContext, ...)`**，无 session/sqlite）；`run_turn` 单次调用只做一轮模型请求，仅处理 `human_message/tool_message` 两种输入；当模型产出 `tool_calls` 时只写 `pending_tool_calls` 并发 `tool_call`，不处理审批、不执行工具、不处理 `tool_result` 回灌；维护 **`ctx.run_turn_phase`** 与跨回合 **`ctx.tool_loop_count`**；**`_request_model_stream`** 在 **`LLM_STREAM_INCLUDE_USAGE`** 开启时转发 `usage`；**`flush_cancelled_turn`**

## `model.py`

- **`get_openai_client`**
- **`get_model_config`**

## `prompt.py`

- **`PROMPT_CONTEXT_DIR`**：仓库根下与 **`app/`** 同级的 **`prompt_context/`**（**`SOUL_MD`** / **`USER_MD`** / **`CUSTOM_MD`**）
- **`_read_prompt_context_markdown`**：读该目录下 **`.md`**（mtime 缓存）
- **`get_static_system_prompt`**
- **`_current_os_kind`**
- **`get_system_prompt(context)`**：静态 + **`soul.md`** + **`user.md`** +（从 `context.loaded_skills` 读取）skills 元数据/正文 + 运行环境 + **`custom.md`**（**`## 自定义补充`**，最末）
- **`read_memory_file_cached`**

侧车目录 **`prompt_context/`** 不在本包内，见仓库根目录 **`prompt_context/README.md`**。

