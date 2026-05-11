# `app/harness/tools/`

| 文件 | 说明 |
|------|------|
| **`tool.py`** | 工具统一入口：`tool` 装饰器 + `get_tools()` + OpenAI tool 适配（`build_openai_toolkit`/`parse_tool_arguments`） |
| **`bash.py`** | 统一 shell 工具 **`bash_run`**（支持 `bash/cmd/powershell`，含分 shell 解析与策略校验） |
| **`host_platform.py`** | **`host_platform`**：查询宿主机 OS（`os_kind` + `platform` 摘要，供与 bash 路径对齐） |
| **`fs.py`** | 文件四件套：**`read_file`** / **`write_file`** / **`edit_file`** / **`search_file`**（`FS_ROOT` 路径约束、按后缀读取策略、行级编辑与关键字检索） |
| **`skills.py`** | 技能加载工具：**`load_skills`**（按 `skill_names` 数组加载会话 skills，并返回已加载与可用技能元数据） |
| **`agent_peer.py`** | Agent 间协作工具：**`agent_discover`**（内含固定结构 `agent_card`，含访问 URL/端口） / **`agent_send_message`**（异步后台提交，结构化汇总对端 SSE，含 `approvals` 摘要与真实 `task.state`） / **`agent_broadcast`**（并发收集分组目标输出，按目标聚合 `approvals` 与 `final_state`） / **`agent_peer_approve_tools`**（对端 `approval_required` 后提交 `approve/reject/selection` 决策，并继续收集对端 SSE） |
| **`async_store.py`** | 异步工具结果仓库：托管后台协程任务，记录 `job_id/status/result/error` |
| **`REFERENCE.md`** | 本目录 Python 符号索引 |
