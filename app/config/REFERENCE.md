# `app/config/` REFERENCE

## `settings.py`

- **`Settings`**：**Pydantic `BaseModel`（frozen）**；全局配置（含 LLM、**`api_host`** / **`api_port`** / 推导 **`agent_api_base`**、**`summary_compression_*`**、**`llm_stream_include_usage`**、**`metrics_enabled`**、**`app_log_level`**、队列、**`fs_tool_*`**、**`agent_id`**、**`registry_url`**、**`discovery_groups`**、**`agent_public_base_url`**、**`agent_peer_*`**、会话持久化等）
- **`_resolve_api_host`** / **`_resolve_api_port`**：解析 **`API_HOST`** / **`API_PORT`**
- **`_resolve_agent_api_base`**：由监听地址推导 CLI HTTP base URL（**`0.0.0.0`** 绑定回落 **`127.0.0.1`**）
- **`get_settings`**：配置单例读取（支持 `reload=True`）
- **`_resolve_agent_id`**：按“**`AGENT_ID` 环境变量** > **文件** > **生成 UUID**”顺序解析并持久化 Agent ID；文件路径固定为 **`runtime_layout.agent_id_file_path()`**（**`<运行根>/.runtime/agent/agent_id`**）
- **`_env_csv`**：解析逗号分隔环境变量并做去重规范化

## `runtime_layout.py`

- **`skills_dir`** / **`raw_message_history_dir`** / **`session_sqlite_path`** / **`agent_id_file_path`** / **`shell_policy_dir`** / **`tool_policy_file_path`**：相对 **`resolve_runtime_root()`** 的固定 **`.runtime/...`** 路径（**不由环境变量覆盖**）

## `logging_setup.py`

- **`resolve_log_level`**：字符串 → logging 数值级别（非法回退 INFO）
- **`numeric_level_to_uvicorn`**：数值级别 → **`uvicorn.run(log_level=...)`** 字符串
- **`configure_app_logging`**：配置 root **`basicConfig`**（stderr）、按需压低 **`httpx`** / **`httpcore`** 噪声

## `env.py`

- **`resolve_runtime_root`**：仓库根或打包后可执行文件目录（绝对路径）
- **`load_env`**：从仓库根 `.env` 读取环境变量

## `host_snapshot.py`

- **`HostSnapshot`**：启动时刻不可变环境快照（os_kind、登录名、euid/egid、`platform.*` 等）
- **`capture_host_snapshot_at_startup`**：构建快照、写入模块缓存并 **`logging.info`** 一条汇总
- **`get_host_snapshot`**：返回缓存；若无缓存则惰性构建（不写启动 INFO）

## `startup_checks.py`

- **`emit_linux_cross_user_shell_startup_hints`**：基于 **`get_host_snapshot()`** 判断 Linux/euid；root / 非 root 分支各打一条 **`logging`** WARNING（不 **`stderr` 再写一遍**，避免与 logging 同屏重复）
