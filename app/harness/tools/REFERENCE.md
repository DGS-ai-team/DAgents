# `app/harness/tools/` REFERENCE

## `tool.py`

- **`get_tools`**
- **`tool`**：项目内工具装饰器，为函数挂载 `name/description` 供注册层读取。
- **`_decorate_sync_tool`**：同步工具装饰路径，仅注入元数据。
- **`_decorate_async_tool`**：异步工具装饰路径，提交后台任务并返回 ACK。
- **`ToolApprovalDecision`**：结构化审批决策，包含 `require_approval`、`reason`、`risk_level`、`mode`。
- **`decide_tool_approval`**：统一审批入口；按全局模式、工具策略与 shell 策略决定是否审批，并返回原因、风险等级与策略来源。
- **`should_require_tool_approval`**：兼容旧调用的布尔审批入口，内部读取 **`decide_tool_approval(...).require_approval`**。
- **`_resolve_repo_relative_path`**：通用「相对运行根 / 绝对路径」解析；工具与 shell 审批路径直接使用 **`runtime_layout.tool_policy_file_path()`** / **`shell_policy_dir()`**。
- **`OpenAIToolSpec`**：**Pydantic `BaseModel`（frozen，`arbitrary_types_allowed`）**；工具规格与 `invoke` 绑定。
- **`_validate_tool_arguments`**：调用工具声明的 Pydantic `args_schema` 做运行时字段校验，并保留 schema 默认值。
- **`_annotation_to_json_schema`**：将 Python 类型注解映射为 JSON Schema，覆盖基础类型、`list[T]`、`dict`、`Literal`、`Optional/Union`。
- **`_signature_to_json_schema`**：函数签名到 OpenAI tool 参数 JSON Schema（默认 `additionalProperties=false`）。
- **`build_openai_toolkit`**：构建 OpenAI tools payload 与执行映射。
- **`parse_tool_arguments`**：解析 tool arguments（JSON 字符串/对象）。

## `async_store.py`

- **`AsyncToolJob`**：异步工具任务快照（含 **`session_id` / `client_id`**、状态、时间戳、结果/错误）。
- **`AsyncToolResultStore`**：后台托管协程并维护任务状态与完成回调。
- **`AsyncToolResultStore.submit_coroutine`**：要求非空 **`client_id`**，与 **`OpenAIConversationContext.sse_client_id`** 对齐，供终态回灌 **`MessageEnvelope.client_id`**。
- **`AsyncToolResultStore.register_message_queue_sender`**：注册消息队列发送器；终态 **`payload`** 含 **`client_id`**，与 **`AsyncToolJob`** 一致。
- **`AsyncToolResultStore.cancel_job`**：请求取消未终态任务并返回快照。
- **`get_async_tool_result_store`**：返回进程级异步工具结果仓库单例。

## `async_tasks.py`

- **`async_tool_status`**：按 job_id 查询异步工具任务快照。
- **`async_tool_cancel`**：按 job_id 请求取消异步工具任务。

## `agent_peer.py` / `agent_peer_common.py` / `agent_peer_registry.py`

- **`agent_peer.py`**：公开工具入口，保留 **`agent_discover` / `agent_send_message` / `agent_broadcast` / `agent_peer_approve_tools`** 注册面。
- **`agent_peer_common.py`**：公共模型与无状态 helper：**`PeerApprovalEntry`**、**`PeerStreamSummary`**、SSE 回放汇总（带 **`Last-Event-ID: -1`**）、A2A token header、错误信封、resume 决策构造、任务状态映射。
- **`agent_peer_registry.py`**：Register Center 解析与目录缓存：按分组发现、TTL 缓存、目标 Agent 解析、Agent Card 摘要补充。
- **`agent_discover`**：按分组发现可协作 Agent（内联固定结构 `agent_card`）。
- **`agent_send_message`**：点对点向目标 Agent 提交消息；支持 **`direct` / `relay`**；返回信封含 `target_session_id/approvals/final_state` 与真实 `task.state`。
- **`agent_broadcast`**：调用 Register Center 广播并并发收集每个目标 SSE，聚合 `approvals` 与广播级 `task.state`。
- **`agent_peer_approve_tools`**：对端 `approval_required` 后提交 `approve/reject/selection` 的 `resume`；支持 **`direct` / `relay`**，再收集后续 SSE。

## `bash.py`

- **`_clip_text`**：按字符上限裁剪输出文本。
- **`ShellJob`**：后台 shell job 快照；保存同一运行中 `Popen` 进程、状态、输出与异步回灌 job_id。
- **`CommandAstNode`**：**Pydantic `BaseModel`（frozen）**；轻量 AST 节点（解析器、片段原文、命令首词）
- **`_split_bash_statements`**：按 bash 规则切分命令片段。
- **`_split_cmd_statements`**：按 cmd 规则切分命令片段。
- **`_split_powershell_statements`**：按 powershell 规则切分命令片段。
- **`_extract_root_for_shell`**：按 shell 类型提取命令首词。
- **`_parse_command_ast`**：将命令解析为轻量 AST 节点列表。
- **`_blocked_non_root_password_prompting_shell`**：基于 **`get_host_snapshot()`** 判定 OS/euid；非 root + bash 时拦截 **`su - … -c`** 及未带 **`-n`/`--non-interactive`** 的 **`sudo`/`sudoedit`**（否则 `None`）
- **`_run_bash_command`**、**`_run_cmd_command`**、**`_run_powershell_command`**：三种 shell 的独立执行方法。
- **`_run_by_shell_type`**：按 shell 类型分发执行方法。
- **`_popen_by_shell_type`** / **`_wait_shell_job`** / **`_terminate_shell_job_process`**：启动可托管进程、后台等待同一进程完成并生成异步回灌摘要、取消后台进程。
- **`bash_run`**：统一入口，支持 `shell_type` 选择执行器并返回结构化结果；同步超时时不杀进程，登记为 ShellJob 并返回 `job_id`/`async_job_id`。
- **`bash_job_status`** / **`bash_job_tail`** / **`bash_job_cancel`**：查询、读取尾部输出、取消后台 ShellJob。

## `host_platform.py`

- **`HostOsKind`**：宿主机 OS 粗分类枚举。
- **`detect_host_os`**：返回当前进程视角的 `HostOsKind`。
- **`host_platform_facts`**：结构化宿主机与 Python 环境字段。
- **`host_platform_summary_text`**：多行可读摘要。
- **`host_platform`**（工具）：返回 JSON + 摘要，供 Agent 区分操作系统。

## `fs.py`

- **`read_file`**（流式行窗口；头含 **`next_line_offset`**；体积上限 **`Settings.fs_tool_read_max_bytes`**）、**`write_file`**、**`search_replace`**、**`search_file`**（流式扫描；**`next_index_offset`**；上限 **`fs_tool_search_max_bytes`**）
- **`_iter_file_lines`**、**`_read_line_window`**、**`_scan_regex_hits`**、**`_merge_line_ranges`**、**`_load_lines_for_ranges`**、**`_read_file_text`**、**`_unified_diff_body`**：流式读、命中扫描与 diff

## `skills.py`

- **`load_skills`**：按 `skill_names` 数组整组设置会话技能；空数组表示清空；返回 `action=set_loaded_skills`、`loaded_skills` 与 `available_skills`。
- **`unload_skills`**：从当前会话已加载技能中移除指定名称，不影响磁盘 skill 文件。
- **`clear_skills`**：清空当前会话已加载技能，不影响磁盘 skill 文件。

## `result_policy.py`

- **`ToolResultEnvelope`**：工具结果三路产物，区分 `model_content`、`display_content`、`raw_ref`、截断与脱敏标记。
- **`package_tool_result`**：对工具输出做敏感信息脱敏、首尾裁剪与 `.runtime/tool_outputs/<id>.txt` 引用；敏感命中时 raw_ref 也只保存脱敏副本，非敏感长输出保留完整内容便于排障。
- **`_filter_sensitive_text`** / **`_clip_middle`**：内置敏感字段过滤与长文本首尾保留裁剪。

