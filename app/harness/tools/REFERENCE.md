# `app/harness/tools/` REFERENCE

## `tool.py`

- **`get_tools`**

## `tooling.py`

- **`tool`**：项目内工具装饰器，为函数挂载 `name/description` 供注册层读取。
- **`_decorate_sync_tool`**：同步工具装饰路径，仅注入元数据。
- **`_decorate_async_tool`**：异步工具装饰路径，提交后台任务并返回 ACK。

## `async_store.py`

- **`AsyncToolJob`**：异步工具任务快照（含 `session_id`、状态、时间戳、结果/错误）。
- **`AsyncToolResultStore`**：后台托管协程并维护任务状态与完成回调。
- **`AsyncToolResultStore.register_message_queue_sender`**：注册消息队列发送器，任务终态时投递 `async_tool_result` 载荷。
- **`get_async_tool_result_store`**：返回进程级异步工具结果仓库单例。

## `agent_peer.py`

- **`agent_discover`**：按分组发现可协作 Agent（支持可选能力标签过滤，并内联固定结构 `agent_card`）
- **`agent_send_message`**：异步点对点向目标 Agent 提交消息（投递链路由配置 `AGENT_PEER_DELIVERY_MODE` 控制）
- **`agent_broadcast`**：异步调用 register-center 广播接口进行分组广播
- **`_collect_peer_stream_output`**：读取目标 `/v1/streams?client_id=...` 并按 `session_id` 汇总可读输出，支持超时截断
- **`_extract_sse_text_from_event`**：从单条 SSE 事件中提取对 Agent 可读正文
- **`_session_id_from_context`**：从 `OpenAIConversationContext` 解析会话 ID，缺失时回退生成。
- **`_cache_agent_list`**：刷新进程内 agent 列表缓存（按 `agent_id` 去重）。
- **`_is_agent_list_cache_stale`**：按 TTL 判断 agent 列表缓存是否过期。
- **`_refresh_agent_list_for_visible_groups`**：按可见分组回源刷新缓存。
- **`_resolve_target_agent_from_cache`**：按可见分组从缓存中解析目标 Agent。
- **`_clear_agent_list_cache`**：清空缓存（测试隔离）。
- **`_discover_agents_by_groups`**：按分组聚合目录查询结果并去重
- **`_attach_agent_card_summary`**：为发现结果补充固定结构 `agent_card`（含访问 URL/端口、card 内容与错误字段）
- **`_resolve_target_agent`**：在调用方可见分组内解析目标 Agent

## `bash.py`

- **`_clip_text`**：按字符上限裁剪输出文本。
- **`CommandAstNode`**：**Pydantic `BaseModel`（frozen）**；轻量 AST 节点（解析器、片段原文、命令首词）
- **`_policy_dir`**：返回策略目录（`SHELL_POLICY_DIR` 或默认 `.agent/policy/shell`）。
- **`_ensure_policy_files`**：确保三套 shell（bash/cmd/powershell）策略文件存在。
- **`_read_roots_file`**：从文本名单文件读取命令首词集合。
- **`_load_policy_sets`**：按 shell 类型加载 `allow/deny` 集合。
- **`_split_bash_statements`**：按 bash 规则切分命令片段。
- **`_split_cmd_statements`**：按 cmd 规则切分命令片段。
- **`_split_powershell_statements`**：按 powershell 规则切分命令片段。
- **`_extract_root_for_shell`**：按 shell 类型提取命令首词。
- **`_parse_command_ast`**：将命令解析为轻量 AST 节点列表。
- **`_validate_command_policy`**：基于文件黑白名单校验命令，并返回待确认命令。
- **`_confirm_non_whitelist_commands`**：对白名单外命令发起 `interrupt` 人工确认。
- **`_run_bash_command`**、**`_run_cmd_command`**、**`_run_powershell_command`**：三种 shell 的独立执行方法。
- **`_run_by_shell_type`**：按 shell 类型分发执行方法。
- **`bash_run`**：统一入口，支持 `shell_type` 选择执行器并返回结构化结果。

## `openai_tools.py`

- **`OpenAIToolSpec`**：**Pydantic `BaseModel`（frozen，`arbitrary_types_allowed`）**；工具规格与 `invoke` 绑定
- **`build_openai_toolkit`**：构建 OpenAI tools payload 与执行映射。
- **`parse_tool_arguments`**：解析 tool arguments（JSON 字符串/对象）。

## `host_platform.py`

- **`HostOsKind`**：宿主机 OS 粗分类枚举。
- **`detect_host_os`**：返回当前进程视角的 `HostOsKind`。
- **`host_platform_facts`**：结构化宿主机与 Python 环境字段。
- **`host_platform_summary_text`**：多行可读摘要。
- **`host_platform`**（工具）：返回 JSON + 摘要，供 Agent 区分操作系统。

## `fs.py`

- **`fs_read`**、**`fs_write`**、**`fs_edit`**

## `common.py`

- **`calc_add`**

