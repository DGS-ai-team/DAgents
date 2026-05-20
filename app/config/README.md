# `app/config/`

| 文件 | 说明 |
|------|------|
| **`settings.py`** | **`Settings`** dataclass、**`get_settings()`**；环境变量键名与 **`.env.example`** 一致 |
| **`runtime_layout.py`** | **`.runtime/...`** 下固定相对路径（skills / JSONL / sqlite / agent_id / 策略文件等），统一锚定 **`resolve_runtime_root()`** |
| **`env.py`** | **`load_env`**；**`resolve_runtime_root`**（仓库根或打包后可执行目录） |
| **`host_snapshot.py`** | **`HostSnapshot`**、**`capture_host_snapshot_at_startup`**（API 启动采集环境并打 INFO）、**`get_host_snapshot`**（进程内只读缓存） |
| **`logging_setup.py`** | **`configure_app_logging`** / **`resolve_log_level`**：与 **`APP_LOG_LEVEL`**、`uvicorn` 对齐的根日志配置 |
| **`startup_checks.py`** | **`emit_linux_cross_user_shell_startup_hints`**：Linux 下跨用户 shell 提示（读 **`get_host_snapshot()`**；仅 **`logging`**，避免 stderr 双写重复） |
## 本地运行时路径（默认均在 `.runtime/`）

下列路径由 **`app/config/runtime_layout.py`** 写死为相对 **`resolve_runtime_root()`** 的片段（**无**对应环境变量覆盖）：

- **`.runtime/memory/session.sqlite3`**：会话 SQLite；是否启用由 **`AGENT_SESSION_STORE_ENABLED`**（**`Settings.agent_session_store_enabled`**）控制
- **`.runtime/agent/agent_id`**：Agent ID 持久化文件
- **`.runtime/history/`**：原始消息 JSONL（开关 **`AGENT_RAW_MESSAGE_HISTORY_ENABLED`**）
- **`.runtime/skills/`**：技能根目录

其它约定路径仍相对运行根：
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
- Agent ID 文件路径固定为 **`<运行根>/.runtime/agent/agent_id`**（见 **`runtime_layout`**）
- 启动时若 `AGENT_ID` 与文件内容都不可用，会自动生成 UUID 并写入文件

## Agent 间协作配置

- `REGISTRY_URL`：Register Center 地址（供 `agent_discover/agent_send_message/agent_broadcast` 使用）
- `DISCOVERY_GROUPS`：当前 Agent 所属分组（逗号分隔，运行时解析为列表）
- `AGENT_PUBLIC_BASE_URL`：当前 Agent 对外可访问地址（用于 API 启动时向 Register Center 自登记）
- `AGENT_REGISTRY_TTL_SECONDS`：Register Center 自登记 TTL 秒数（默认 `60`，API 会按半 TTL 续租）
- `AGENT_PEER_SHARED_TOKEN`：可选 A2A 共享令牌；配置后 Register Center、Agent 入站 A2A 与 A2A SSE 需携带 `x-dagents-a2a-token`
- `AGENT_PEER_CACHE_TTL_SECONDS`：`agent_peer` 中 agent 列表缓存 TTL 秒数（默认 `60`）
- `AGENT_PEER_DELIVERY_MODE`：`agent_send_message` / `agent_peer_approve_tools` 投递模式（`direct` 直连目标，`relay` 经 Register Center 中继；默认 `direct`）
- `AGENT_PEER_HTTP_RETRY_ATTEMPTS`：A2A 只读 HTTP 请求最大尝试次数（默认 `2`，范围 `1..5`；不重试非幂等消息 POST）
- `AGENT_PEER_STREAM_TIMEOUT_SECONDS`：`agent_send_message` 拉取对端 SSE 输出的超时秒数（默认 `60`）
- `AGENT_PEER_BROADCAST_STREAM_TIMEOUT_SECONDS`：`agent_broadcast` 汇总多目标 SSE 输出的总超时秒数（默认 `20`，超时截断已收集内容）
- `REGISTER_CENTER_STORE_PATH`：Register Center 服务自身可选 JSON 持久化路径；未配置时仍为进程内内存表
