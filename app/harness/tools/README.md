# `app/harness/tools/`

| 文件 | 说明 |
|------|------|
| **`tool.py`** | 工具统一入口：`tool` 装饰器 + `get_tools()` + OpenAI tool 适配（`build_openai_toolkit`/`parse_tool_arguments`/运行时 args_schema 校验/结构化审批决策）；**当前模型可见工具清单**见 [built-in-tools.md](../../../doc/built-in-tools.md) |
| **`bash.py`** | 统一 shell 工具 **`bash_run`**（支持 `bash/cmd/powershell`，超时自动降级为后台 ShellJob），以及 **`bash_job_status`** / **`bash_job_tail`** / **`bash_job_cancel`** |
| **`host_platform.py`** | **`host_platform`**：查询宿主机 OS（`os_kind` + `platform` 摘要，供与 bash 路径对齐） |
| **`fs.py`** | 文件工具：**`read_file`**（流式分页 + `next_line_offset`）/ **`search_replace`** / **`search_file`**（流式 + `next_index_offset`）/ **`write_file`** |
| **`skills.py`** | 技能生命周期工具：**`load_skills`**（整组替换/空数组清空）、**`unload_skills`**（移除指定已加载技能）、**`clear_skills`**（清空会话技能） |
| **`result_policy.py`** | 工具结果过滤/压缩：模型上下文、SSE 展示与原始落盘引用三路分离 |
| **`async_tasks.py`** | 异步工具任务查询与取消：**`async_tool_status`** / **`async_tool_cancel`** |
| **`agent_peer.py`** | Agent 间协作工具：**`agent_discover`**（内含固定结构 `agent_card`，含访问 URL/端口） / **`agent_send_message`**（异步后台提交，结构化汇总对端 SSE，含 `approvals` 摘要与真实 `task.state`） / **`agent_broadcast`**（并发收集分组目标输出，按目标聚合 `approvals` 与 `final_state`） / **`agent_peer_approve_tools`**（对端 `approval_required` 后提交 `approve/reject/selection` 决策，并继续收集对端 SSE） |
| **`async_store.py`** | 异步工具结果仓库：托管后台协程任务，记录 `job_id/status/result/error` |
| **`REFERENCE.md`** | 本目录 Python 符号索引 |
