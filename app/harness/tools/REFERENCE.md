# `app/harness/tools/` REFERENCE

## `tool.py`

- **`get_tools`**
- **`tool`**：项目内工具装饰器，为函数挂载 `name/description` 供注册层读取。
- **`_decorate_sync_tool`**：同步工具装饰路径，仅注入元数据。
- **`_decorate_async_tool`**：异步工具装饰路径，提交后台任务并返回 ACK。
- **`should_require_tool_approval`**：统一审批入口；按全局模式、工具策略与 shell 策略决定是否审批。
- **`OpenAIToolSpec`**：**Pydantic `BaseModel`（frozen，`arbitrary_types_allowed`）**；工具规格与 `invoke` 绑定。
- **`build_openai_toolkit`**：构建 OpenAI tools payload 与执行映射。
- **`parse_tool_arguments`**：解析 tool arguments（JSON 字符串/对象）。

## `async_store.py`

- **`AsyncToolJob`**：异步工具任务快照（含 `session_id`、状态、时间戳、结果/错误）。
- **`AsyncToolResultStore`**：后台托管协程并维护任务状态与完成回调。
- **`AsyncToolResultStore.register_message_queue_sender`**：注册消息队列发送器，任务终态时投递 `async_tool_result` 载荷。
- **`get_async_tool_result_store`**：返回进程级异步工具结果仓库单例。

## `agent_peer.py`

- **`PeerApprovalEntry`**：**Pydantic `BaseModel`**；对端 `approval_required` 事件结构化条目（含 `target_session_id/approval_id/display_type/approval_args` 等）。
- **`PeerStreamSummary`**：**Pydantic `BaseModel`**；单次远端 SSE 拉取汇总（`text/approvals/errors/final_state/truncated`）。
- **`agent_discover`**：按分组发现可协作 Agent（内联固定结构 `agent_card`）
- **`agent_send_message`**：点对点向目标 Agent 提交消息；返回信封含 `target_session_id/approvals/final_state` 与真实 `task.state`
- **`agent_broadcast`**：调用 Register Center 广播并并发收集每个目标 SSE，聚合 `approvals` 与广播级 `task.state`
- **`agent_peer_approve_tools`**：对端 `approval_required` 后向其提交 `approve/reject/selection` 决策的 `resume`，再收集后续 SSE
- **`_collect_peer_stream_summary`**：读取目标 `/v1/streams?client_id=...` 并按 `session_id` 汇总文本/审批/错误，超时返回 `truncated`
- **`_approval_entry_from_event`**：把 SSE `approval_required` 的 `data` 转为 `PeerApprovalEntry`
- **`_peer_state_to_task_state`**：把 `PeerStreamSummary.final_state` 映射到 `AgentPeerTaskState`
- **`_build_resume_value`**：按 `decision` 构造与 `app.schemas.approval` 对齐的 `resume_value`（`approve/reject/selection`）
- **`_new_peer_session_id`**：为单次点对点请求生成隔离的对端会话 ID（`peer-<caller>-<target>-<short>`）
- **`_session_id_from_context`**：从 `OpenAIConversationContext` 解析调用方会话 ID，缺失时回退生成
- **`_cache_agent_list`** / **`_is_agent_list_cache_stale`** / **`_refresh_agent_list_for_visible_groups`** / **`_resolve_target_agent_from_cache`** / **`_clear_agent_list_cache`**：进程内 agent 目录缓存与 TTL 维护
- **`_discover_agents_by_groups`**：按分组聚合目录查询结果并去重
- **`_attach_agent_card_summary`**：为发现结果补充固定结构 `agent_card`（含访问 URL/端口、card 内容与错误字段）
- **`_resolve_target_agent`**：在调用方可见分组内解析目标 Agent

## `bash.py`

- **`_clip_text`**：按字符上限裁剪输出文本。
- **`CommandAstNode`**：**Pydantic `BaseModel`（frozen）**；轻量 AST 节点（解析器、片段原文、命令首词）
- **`_split_bash_statements`**：按 bash 规则切分命令片段。
- **`_split_cmd_statements`**：按 cmd 规则切分命令片段。
- **`_split_powershell_statements`**：按 powershell 规则切分命令片段。
- **`_extract_root_for_shell`**：按 shell 类型提取命令首词。
- **`_parse_command_ast`**：将命令解析为轻量 AST 节点列表。
- **`_blocked_non_root_password_prompting_shell`**：基于 **`get_host_snapshot()`** 判定 OS/euid；非 root + bash 时拦截 **`su - … -c`** 及未带 **`-n`/`--non-interactive`** 的 **`sudo`/`sudoedit`**（否则 `None`）
- **`_run_bash_command`**、**`_run_cmd_command`**、**`_run_powershell_command`**：三种 shell 的独立执行方法。
- **`_run_by_shell_type`**：按 shell 类型分发执行方法。
- **`bash_run`**：统一入口，支持 `shell_type` 选择执行器并返回结构化结果。

## `host_platform.py`

- **`HostOsKind`**：宿主机 OS 粗分类枚举。
- **`detect_host_os`**：返回当前进程视角的 `HostOsKind`。
- **`host_platform_facts`**：结构化宿主机与 Python 环境字段。
- **`host_platform_summary_text`**：多行可读摘要。
- **`host_platform`**（工具）：返回 JSON + 摘要，供 Agent 区分操作系统。

## `fs.py`

- **`read_file`**、**`write_file`**、**`edit_file`**、**`search_file`**

## `skills.py`

- **`load_skills`**：按 `skill_names` 数组加载会话技能；返回 `loaded_skills` 与 `available_skills` 元数据 JSON（字段为 `skill_name/description`）

