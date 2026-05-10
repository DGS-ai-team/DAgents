# `app/config/` REFERENCE

## `settings.py`

- **`Settings`**：**Pydantic `BaseModel`（frozen）**；全局配置（含 LLM、**`summary_compression_silent_trigger_tokens`**、**`summary_compression_blocking_trigger_tokens`**、**`llm_stream_include_usage`**、**`metrics_enabled`**、队列（**`max_queue_size`** / **`agent_max_active_session_queues`** / **`agent_session_idle_evict_seconds`**）、CLI、**`agent_id`**、**`agent_id_file_path`**、**`registry_url`**、**`discovery_groups`**、**`agent_public_base_url`**、**`agent_peer_cache_ttl_seconds`**、**`agent_peer_delivery_mode`**、**`agent_peer_stream_timeout_seconds`**、**`agent_peer_broadcast_stream_timeout_seconds`**、**`agent_session_store_path`**、**`agent_raw_message_history_enabled`**、**`agent_raw_message_history_dir`**）
- **`get_settings`**：配置单例读取（支持 `reload=True`）
- **`_agent_id_file_path`**：解析 Agent ID 文件路径（含默认值回退）
- **`_resolve_agent_id`**：按“环境变量 > 文件 > 生成 UUID”顺序解析并持久化 Agent ID
- **`_env_csv`**：解析逗号分隔环境变量并做去重规范化

## `env.py`

- **`load_env`**：从仓库根 `.env` 读取环境变量

## `host_snapshot.py`

- **`HostSnapshot`**：启动时刻不可变环境快照（os_kind、登录名、euid/egid、`platform.*` 等）
- **`capture_host_snapshot_at_startup`**：构建快照、写入模块缓存并 **`logging.info`** 一条汇总
- **`get_host_snapshot`**：返回缓存；若无缓存则惰性构建（不写启动 INFO）

## `startup_checks.py`

- **`emit_linux_cross_user_shell_startup_hints`**：基于 **`get_host_snapshot()`** 判断 Linux/euid；root / 非 root 分支各打一条 **`logging`** WARNING（不 **`stderr` 再写一遍**，避免与 logging 同屏重复）

