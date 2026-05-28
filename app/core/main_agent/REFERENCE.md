# `app/core/main_agent/` REFERENCE

## `display_inference.py`

- **`VALID_DISPLAY_TYPES`**：SSE **`display_type`** 合法字面量集合（含 **`reasoning`**、**`markdown`**）
- **`infer_assistant_delta_display_type`**：assistant 流式分片 → **`image`** / **`code`** / **`markdown`**
- **`infer_reasoning_delta_display_type`**：推理流 → **`reasoning`**
- **`infer_tool_call_display_type`**：本轮 **`assistant_content`** + **`tool_calls`** → 汇总类型
- **`infer_tool_result_display_type`**：工具执行结果（工具名 + 正文）

## `agent.py`

- **`init_agent`**：创建并返回 OpenAI 隐式 ReAct runtime
- **`MainAgentTurnOrchestrator`**：消息回合业务编排器；`resume` / `async_tool_result` / `tool_result` / `human_message` 分支；装配 **`SummaryCompressionCoordinator`**、**`ToolExecutionCoordinator`**、**`ToolResumeCoordinator`**；**`_invoke_tool`** 内在得到最终 **`result_text`** 后经 **`package_tool_result`** 裁剪/脱敏并 **`emit` `tool_result`** 信封；**`_handle_async_tool_result`** 合成 `tool_callback` assistant，`reasoning_content` 由统一 message 写入口补齐；**`_handle_tool_result`** 仅驱动 **`tool_message`** 下一轮（不再重复发 **`tool_result`** SSE）；**`_handle_human_message`**：若 **`ctx.pending_tool_calls`** 非空则按 pending 逐条补打断 **`tool`/`tool_result` SSE** 后 **`clear()`** pending 并 **`run_turn_phase=IDLE`**；**`display_inference`** 生成 **`tool_result` / `tool_call` / `approval_required`** 等 **`display_type`**

## `summary_compression.py`

- **`SummaryCompressionCoordinator`**：上下文压缩协调器；维护按 session 的静默压缩 task 与待应用结果；基于启动瞬间 **`snapshot_messages`** 生成摘要，应用阶段校验被压缩区间指纹，区间未变但后续只追加消息时允许无感应用。
- **`snapshot_messages`**：将 OpenAI messages 深拷贝为压缩输入快照，避免后台摘要读取正在变更的列表。
- **`CompressionApplyResult`**：压缩应用结果（applied / stale / invalid）与压缩原消息条数。

## `tool_execution.py`

- **`ToolExecutionPlan`**：单轮 `tool_calls` 的内部执行计划，承载自动执行工具、待审批工具与每个 call 的 **`ToolApprovalDecision`**。
- **`build_tool_execution_plan`**：根据 pending tool call 与 **`decide_tool_approval`** 生成计划；识别 **`ask_user_information`** 优先等待用户输入；同批只要有一项需审批，则整批等待用户 resume。
- **`build_user_information_required_payload`** / **`wait_for_user_information`**：发出 `user_information_required` SSE 并暂停回合。
- **`pending_tool_call_to_approval_item`** / **`build_approval_required_payload`**：构造审批卡片数据，透出 **`approval_reason`**、**`risk_level`**、**`approval_mode`** 等元数据。
- **`ToolExecutionCoordinator`**：统一处理审批等待批与自动执行批，自动执行结果会写回 tool message 并以 `tool_result` 请求重新入队。

## `tool_resume.py`

- **`ResumeDecisionPlan`**：审批恢复计划，包含批准/拒绝 call_id 集合以及非法输入的错误原因。
- **`ToolResumeCoordinator`**：处理 approve/reject/selective resume；selection 必须一次性覆盖全部 pending tool calls，避免 OpenAI tool-call pairing 被半闭合状态破坏。

## `runtime_openai.py`

- **`OpenAIImplicitReActRuntime`**：OpenAI 原生 tool calling 运行时（仅推理：**`run_turn(ctx: OpenAIConversationContext, ...)`**，无 session/sqlite）；写入 **`ctx.messages`** 的 assistant 行始终带 **`reasoning_content`**（无思维链时为空串），供 DeepSeek 思考模式 tool 链回传；`run_turn` 单次调用只做一轮模型请求，仅处理 `human_message/tool_message` 两种输入；**`human_message`** 时 **`ctx.tool_loop_count = 0`** 再进入模型；**`tool_message`** 沿用累计直至无 **`tool_calls`** 的 assistant 收口时清零；流式阶段 **`run_turn`** 转发 **`assistant` / `reasoning` / `tool_call_delta`**（**`tool_call`** 仍为 **`final`** 后的整包）；**所有 `done` 信封均带 `finish_reason`**（模型侧或 **`empty_content` / `tool_loop_limit` / `model_stream_failed` 等** 语义）；当模型产出 `tool_calls` 时只写 `pending_tool_calls` 并发 **`tool_call`（含 **`display_type`**）**，不处理审批、不执行工具、不处理 `tool_result` 回灌；**`assistant` / `reasoning`** 事件携带 **`display_type`**；维护 **`ctx.run_turn_phase`**；**`_request_model_stream`**：delta 累加 **`final`**（含 **`finish_reason`** 字段）；**`finish_reason`** 流式分片另经 **`done`** 透出；**`LLM_STREAM_INCLUDE_USAGE`** 时转发 **`usage`**，**`DEBUG`** 下对每条流式 chunk 打 **`%r`**；**`flush_cancelled_turn`**

## `model.py`

- **`get_openai_client`**
- **`get_model_config`**

## `prompt.py`

- **`PROMPT_CONTEXT` 侧车目录**：**`<resolve_runtime_root()>/.runtime/prompt_context`**；缺失 **`soul.md` / `user.md` / `custom.md`** 时由 **`_ensure_prompt_context_files_exist`** 创建 **空 UTF-8 文件**（不覆盖已有文件）。发布包内可由 **`packaging/runtime/prompt_context/`** 空文件占位随 zip 解压即存在。
- **`_prompt_context_dir`** / **`_ensure_prompt_context_files_exist`** / **`_read_prompt_context_markdown`**：目录与侧车文件、读盘与 mtime 缓存
- **`get_static_system_prompt`**
- **`build_stable_system_prompt`**：构造可缓存的稳定 system prompt 前缀，包含最高优先级规则、skills 元数据、运行环境与 `.runtime` 工作目录约定；按稳定配置 key 进程内缓存。
- **`build_prompt_context_sections`**：读取较稳定的用户侧上下文（`soul.md`、`user.md`、长期记忆）。
- **`build_loaded_skills_section`**：仅按当前 session 的 `context.loaded_skills` 注入已加载技能正文，避免进入稳定前缀。
- **`build_custom_prompt_context_section`**：读取高频变化的 `custom.md` 临时/专项指令。
- **`build_session_system_suffix`**：追加最易变的 session 环境信息。
- **`_format_runtime_environment_section`**：将 **`HostSnapshot`** 格式化为「当前运行环境」正文（OS 类别、平台摘要、登录名、UID/GID）
- **`_format_runtime_workspace_section`**：**`.runtime`** 子目录约定（含 **`data/`**、**`scripts/`**、**`scripts_menu.md`**）
- **`get_system_prompt(context)`**：按「稳定前缀 → prompt context 侧车 → loaded skills → custom → session 后缀」拼接，平衡 prompt cache 命中与会话差异。
- **`read_memory_file_cached`** / **`_read_long_term_memory`**：只读长期记忆 Markdown，按 mtime 缓存；不存在或空白时不注入 prompt

侧车 Markdown 仅位于 **`<运行根>/.runtime/prompt_context/`**；内容由部署方在本地编辑（初始为空文件）。
