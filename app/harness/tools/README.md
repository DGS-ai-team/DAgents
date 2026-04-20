# `app/harness/tools/`

| 文件 | 说明 |
|------|------|
| **`tool.py`** | `get_tools()`，供 **`main_agent/agent.py`** 使用 |
| **`bash.py`** | 统一 shell 工具 **`bash_run`**（支持 `bash/cmd/powershell`，含分 shell 解析与策略校验） |
| **`host_platform.py`** | **`host_platform`**：查询宿主机 OS（`os_kind` + `platform` 摘要，供与 bash 路径对齐） |
| **`fs.py`** | 文件三件套：**`fs_read`** / **`fs_write`** / **`fs_edit`**（含 FS_ROOT 路径约束与基础 replace 编辑） |
| **`agent_peer.py`** | Agent 间协作工具：**`agent_discover`**（内含固定结构 `agent_card`，含访问 URL/端口） / **`agent_send_message`**（异步后台提交，支持汇总对端 SSE 输出） / **`agent_broadcast`**（异步后台广播，支持超时截断并返回已采集输出） |
| **`tooling.py`** | 项目内工具装饰器 **`tool`**（统一向工具注入 `OpenAIConversationContext`） |
| **`async_store.py`** | 异步工具结果仓库：托管后台协程任务，记录 `job_id/status/result/error` |
| **`openai_tools.py`** | OpenAI tool calling 适配层：将本目录工具转换为 OpenAI tools 规格并执行 |
| **`common.py`** | 常用工具占位模块（用于你后续新增测试工具） |
| **`REFERENCE.md`** | 本目录 Python 符号索引 |
