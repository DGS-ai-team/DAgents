# `app/core/main_agent/` REFERENCE

## `display_inference.py`

- **`VALID_DISPLAY_TYPES`**：SSE **`display_type`** 合法字面量集合（含 **`reasoning`**、**`markdown`**）
- **`infer_assistant_delta_display_type`**：assistant 流式分片 → **`image`** / **`code`** / **`markdown`**
- **`infer_reasoning_delta_display_type`**：推理流 → **`reasoning`**
- **`infer_tool_call_display_type`**：本轮 **`assistant_content`** + **`tool_calls`** → 汇总类型
- **`infer_tool_result_display_type`**：工具执行结果（工具名 + 正文）

## `agent.py`

- **`init_agent`**：创建并返回 OpenAI 隐式 ReAct runtime
- **`MainAgentTurnOrchestrator`**：消息回合业务编排器；`resume` / `async_tool_result` / `tool_result` / `human_message` 分支；`run_turn` 与工具审批/执行、tool_result 回灌；并内聚 summary 压缩入口流程（已完成结果替换、阻塞压缩、静默压缩任务管理）；**`display_inference`** 生成 **`tool_result` / `tool_call` / `approval_required`** 等 **`display_type`**

## `runtime_openai.py`

- **`OpenAIImplicitReActRuntime`**：OpenAI 原生 tool calling 运行时（仅推理：**`run_turn(ctx: OpenAIConversationContext, ...)`**，无 session/sqlite）；`run_turn` 单次调用只做一轮模型请求，仅处理 `human_message/tool_message` 两种输入；当模型产出 `tool_calls` 时只写 `pending_tool_calls` 并发 **`tool_call`（含 **`display_type`**）**，不处理审批、不执行工具、不处理 `tool_result` 回灌；**`assistant` / `reasoning`** 事件携带 **`display_type`**；维护 **`ctx.run_turn_phase`** 与跨回合 **`ctx.tool_loop_count`**；**`_request_model_stream`** 在 **`LLM_STREAM_INCLUDE_USAGE`** 开启时转发 `usage`，并在 **`DEBUG`** 下对每条流式 chunk 打 **`%r`** 原文；**`flush_cancelled_turn`**

## `model.py`

- **`get_openai_client`**
- **`get_model_config`**

## `prompt.py`

- **`PROMPT_CONTEXT_DIR`**：仓库根下与 **`app/`** 同级的 **`prompt_context/`**（**`SOUL_MD`** / **`USER_MD`** / **`CUSTOM_MD`**）
- **`_read_prompt_context_markdown`**：读该目录下 **`.md`**（mtime 缓存）
- **`get_static_system_prompt`**
- **`_format_runtime_environment_section`**：将 **`HostSnapshot`** 格式化为「当前运行环境」正文（OS 类别、平台摘要、登录名、UID/GID）
- **`_format_runtime_workspace_section`**：**`.runtime`** 子目录约定（含 **`data/`**、**`scripts/`**、**`scripts_menu.md`**）
- **`_skills_base_dir_for_prompt`**：skills 根目录绝对路径（配置相对 **`resolve_runtime_root()`**）
- **`get_system_prompt(context)`**：静态 + **`soul.md`** + **`user.md`** + skills + **`get_host_snapshot()`** 运行环境 + **`.runtime` 工作目录约定** +（配置启用时）JSONL 原始消息记录说明 + **`custom.md`**（最末）
- **`read_memory_file_cached`**

侧车目录 **`prompt_context/`** 不在本包内，见仓库根目录 **`prompt_context/README.md`**。

