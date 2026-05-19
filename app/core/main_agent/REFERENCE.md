# `app/core/main_agent/` REFERENCE

## `display_inference.py`

- **`VALID_DISPLAY_TYPES`**：SSE **`display_type`** 合法字面量集合（含 **`reasoning`**、**`markdown`**）
- **`infer_assistant_delta_display_type`**：assistant 流式分片 → **`image`** / **`code`** / **`markdown`**
- **`infer_reasoning_delta_display_type`**：推理流 → **`reasoning`**
- **`infer_tool_call_display_type`**：本轮 **`assistant_content`** + **`tool_calls`** → 汇总类型
- **`infer_tool_result_display_type`**：工具执行结果（工具名 + 正文）

## `agent.py`

- **`init_agent`**：创建并返回 OpenAI 隐式 ReAct runtime
- **`ToolExecutionPlan`**：单轮 `tool_calls` 的内部执行计划，显式承载自动执行工具与待审批工具两组。
- **`MainAgentTurnOrchestrator`**：消息回合业务编排器；`resume` / `async_tool_result` / `tool_result` / `human_message` 分支；`run_turn` 与工具审批/执行、tool_result 回灌；**`_build_tool_execution_plan`** 按审批策略生成 **`ToolExecutionPlan`**；**`_invoke_tool`** 内在得到最终 **`result_text`** 后 **`emit` `tool_result`** 信封；**`_handle_tool_result`** 仅驱动 **`tool_message`** 下一轮（不再重复发 **`tool_result`** SSE）；**`_handle_human_message`**：若 **`ctx.pending_tool_calls`** 非空则按 pending 逐条补打断 **`tool`/`tool_result` SSE** 后 **`clear()`** pending 并 **`run_turn_phase=IDLE`**；并内聚 summary 压缩入口流程（已完成结果替换、阻塞压缩失败可恢复错误、静默压缩 source_fingerprint 版本校验、静默压缩任务管理）；**`display_inference`** 生成 **`tool_result` / `tool_call` / `approval_required`** 等 **`display_type`**

## `runtime_openai.py`

- **`OpenAIImplicitReActRuntime`**：OpenAI 原生 tool calling 运行时（仅推理：**`run_turn(ctx: OpenAIConversationContext, ...)`**，无 session/sqlite）；`run_turn` 单次调用只做一轮模型请求，仅处理 `human_message/tool_message` 两种输入；**`human_message`** 时 **`ctx.tool_loop_count = 0`** 再进入模型；**`tool_message`** 沿用累计直至无 **`tool_calls`** 的 assistant 收口时清零；流式阶段 **`run_turn`** 转发 **`assistant` / `reasoning` / `tool_call_delta`**（**`tool_call`** 仍为 **`final`** 后的整包）；**所有 `done` 信封均带 `finish_reason`**（模型侧或 **`empty_content` / `tool_loop_limit` / `model_stream_failed` 等** 语义）；当模型产出 `tool_calls` 时只写 `pending_tool_calls` 并发 **`tool_call`（含 **`display_type`**）**，不处理审批、不执行工具、不处理 `tool_result` 回灌；**`assistant` / `reasoning`** 事件携带 **`display_type`**；维护 **`ctx.run_turn_phase`**；**`_request_model_stream`**：delta 累加 **`final`**（含 **`finish_reason`** 字段）；**`finish_reason`** 流式分片另经 **`done`** 透出；**`LLM_STREAM_INCLUDE_USAGE`** 时转发 **`usage`**，**`DEBUG`** 下对每条流式 chunk 打 **`%r`**；**`flush_cancelled_turn`**

## `model.py`

- **`get_openai_client`**
- **`get_model_config`**

## `prompt.py`

- **`PROMPT_CONTEXT` 侧车目录**：**`<resolve_runtime_root()>/.runtime/prompt_context`**；缺失 **`soul.md` / `user.md` / `custom.md`** 时由 **`_ensure_prompt_context_files_exist`** 创建 **空 UTF-8 文件**（不覆盖已有文件）。发布包内可由 **`packaging/runtime/prompt_context/`** 空文件占位随 zip 解压即存在。
- **`_prompt_context_dir`** / **`_ensure_prompt_context_files_exist`** / **`_read_prompt_context_markdown`**：目录与侧车文件、读盘与 mtime 缓存
- **`get_static_system_prompt`**
- **`_format_runtime_environment_section`**：将 **`HostSnapshot`** 格式化为「当前运行环境」正文（OS 类别、平台摘要、登录名、UID/GID）
- **`_format_runtime_workspace_section`**：**`.runtime`** 子目录约定（含 **`data/`**、**`scripts/`**、**`scripts_menu.md`**）
- **`get_system_prompt(context)`**：静态 + **`.runtime` 侧车 `soul.md` / `user.md`** + 可选长期记忆 **`.runtime/memory/long_term.md`** + skills + **`get_host_snapshot()`** 运行环境 + **`.runtime` 工作目录约定** +（配置启用时）JSONL 原始消息记录说明 + **`custom.md`** + **`session_id`**（最末）；自主创建 skills 段落中的根路径同 **`runtime_layout.skills_dir()`**
- **`read_memory_file_cached`** / **`_read_long_term_memory`**：只读长期记忆 Markdown，按 mtime 缓存；不存在或空白时不注入 prompt

侧车 Markdown 仅位于 **`<运行根>/.runtime/prompt_context/`**；内容由部署方在本地编辑（初始为空文件）。
