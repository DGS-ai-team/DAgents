# `app/config/`

| 文件 | 说明 |
|------|------|
| **`settings.py`** | **`Settings`** dataclass、**`get_settings()`**；环境变量键名与 **`.env.example`** 一致 |
| **`env.py`** | **`load_env`**；**`resolve_runtime_root`**（仓库根或打包后可执行目录） |
| **`host_snapshot.py`** | **`HostSnapshot`**、**`capture_host_snapshot_at_startup`**（API 启动采集环境并打 INFO）、**`get_host_snapshot`**（进程内只读缓存） |
| **`logging_setup.py`** | **`configure_app_logging`** / **`resolve_log_level`**：与 **`APP_LOG_LEVEL`**、`uvicorn` 对齐的根日志配置 |
| **`startup_checks.py`** | **`emit_linux_cross_user_shell_startup_hints`**：Linux 下跨用户 shell 提示（读 **`get_host_snapshot()`**；仅 **`logging`**，避免 stderr 双写重复） |
## 本地运行时路径（默认均在 `.runtime/`）

配置里的相对路径一律相对 **`resolve_runtime_root()`**（仓库根）。约定：

- **`.runtime/memory/session.sqlite3`**：`AGENT_SESSION_STORE_PATH` 默认
- **`.runtime/agent/agent_id`**：`AGENT_ID_FILE_PATH` 默认
- **`.runtime/history/`**：`AGENT_RAW_MESSAGE_HISTORY_DIR` 默认（原始消息 JSONL）
- **`.runtime/skills/`**：`AGENT_SKILLS_DIR` 默认
- **`.runtime/data/`**：临时数据（脚本输出、上传文件、中间产物等；非唯一权威存档）
- **`.runtime/scripts/`**：与 skills 无绑定的独立脚本优先存放处
- **`.runtime/scripts_menu.md`**：脚本索引与说明（路径、用途、运行方式等），便于检索；增删脚本时请同步更新

仓库根旧 **`history/*.jsonl`**、**`skills/`** 可运行 **`scripts/migrate_runtime_layout.py`** 迁入 **`.runtime/`**。

## OpenAI 隐式 ReAct 配置

- `LLM_MAX_TOOL_LOOPS`：单轮请求中允许的最大工具调用轮次（默认 `16`）
- `SUMMARY_COMPRESSION_SILENT_TRIGGER_TOKENS`：summary 静默压缩触发阈值（估算 token 数，默认 `4000`；`<=0` 关闭静默压缩）
- `SUMMARY_COMPRESSION_BLOCKING_TRIGGER_TOKENS`：summary 阻塞压缩触发阈值（估算 token 数，默认 `8000`；`<=0` 关闭阻塞压缩）

## 日志

- `APP_LOG_LEVEL`：应用根日志与 `run_agent_api.py` 传入 uvicorn 的级别（`DEBUG` / `INFO` / `WARNING` / `ERROR` / `CRITICAL`，默认 `INFO`）。设为 `DEBUG` 时，`OpenAIImplicitReActRuntime` 会以 **`%r`** 输出每条流式 chunk 对象。

## Agent 标识配置

- `AGENT_ID`：可显式指定当前 Agent ID（优先级最高）
- `AGENT_ID_FILE_PATH`：Agent ID 持久化文件路径（默认 **`.runtime/agent/agent_id`**）
- 启动时若 `AGENT_ID` 与文件内容都不可用，会自动生成 UUID 并写入文件

## Agent 间协作配置

- `REGISTRY_URL`：Register Center 地址（供 `agent_discover/agent_send_message/agent_broadcast` 使用）
- `DISCOVERY_GROUPS`：当前 Agent 所属分组（逗号分隔，运行时解析为列表）
- `AGENT_PUBLIC_BASE_URL`：当前 Agent 对外可访问地址（用于 API 启动时向 Register Center 自登记）
- `AGENT_PEER_CACHE_TTL_SECONDS`：`agent_peer` 中 agent 列表缓存 TTL 秒数（默认 `60`）
- `AGENT_PEER_DELIVERY_MODE`：`agent_send_message` 投递模式（`direct` 直连目标，`relay` 经 Register Center 中继；默认 `direct`）
- `AGENT_PEER_STREAM_TIMEOUT_SECONDS`：`agent_send_message` 拉取对端 SSE 输出的超时秒数（默认 `60`）
- `AGENT_PEER_BROADCAST_STREAM_TIMEOUT_SECONDS`：`agent_broadcast` 汇总多目标 SSE 输出的总超时秒数（默认 `20`，超时截断已收集内容）
