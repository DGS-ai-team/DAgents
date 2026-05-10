# `app/config/`

| 文件 | 说明 |
|------|------|
| **`settings.py`** | **`Settings`** dataclass、**`get_settings()`**；环境变量键名与 **`.env.example`** 一致 |
| **`env.py`** | **`load_env`**：将仓库根 **`.env`** 读入 `os.environ`（不覆盖已有变量） |
| **`host_snapshot.py`** | **`HostSnapshot`**、**`capture_host_snapshot_at_startup`**（API 启动采集环境并打 INFO）、**`get_host_snapshot`**（进程内只读缓存） |
| **`startup_checks.py`** | **`emit_linux_cross_user_shell_startup_hints`**：Linux 下跨用户 shell 提示（读 **`get_host_snapshot()`**；仅 **`logging`**，避免 stderr 双写重复） |

## 会话与审计

- `AGENT_SESSION_STORE_PATH`：会话上下文 sqlite 路径（默认 `.runtime/memory/session.sqlite3`；显式空串可关闭）
- `AGENT_RAW_MESSAGE_HISTORY_ENABLED`：是否在每次业务 **`ctx.messages` 追加/插入**时写原始消息 JSONL（默认 `true`；摘要压缩等整段替换不写）
- `AGENT_RAW_MESSAGE_HISTORY_DIR`：审计目录名（相对 **`resolve_runtime_root()`**，默认 `history`）

## OpenAI 隐式 ReAct 配置

- `LLM_MAX_TOOL_LOOPS`：单轮请求中允许的最大工具调用轮次（默认 `16`）
- `SUMMARY_COMPRESSION_SILENT_TRIGGER_TOKENS`：summary 静默压缩触发阈值（估算 token 数，默认 `4000`；`<=0` 关闭静默压缩）
- `SUMMARY_COMPRESSION_BLOCKING_TRIGGER_TOKENS`：summary 阻塞压缩触发阈值（估算 token 数，默认 `8000`；`<=0` 关闭阻塞压缩）

## Agent 标识配置

- `AGENT_ID`：可显式指定当前 Agent ID（优先级最高）
- `AGENT_ID_FILE_PATH`：Agent ID 持久化文件路径（默认 `.runtime/agent/agent_id`）
- 启动时若 `AGENT_ID` 与文件内容都不可用，会自动生成 UUID 并写入文件

## Agent 间协作配置

- `REGISTRY_URL`：Register Center 地址（供 `agent_discover/agent_send_message/agent_broadcast` 使用）
- `DISCOVERY_GROUPS`：当前 Agent 所属分组（逗号分隔，运行时解析为列表）
- `AGENT_PUBLIC_BASE_URL`：当前 Agent 对外可访问地址（用于 API 启动时向 Register Center 自登记）
- `AGENT_PEER_CACHE_TTL_SECONDS`：`agent_peer` 中 agent 列表缓存 TTL 秒数（默认 `60`）
- `AGENT_PEER_DELIVERY_MODE`：`agent_send_message` 投递模式（`direct` 直连目标，`relay` 经 Register Center 中继；默认 `direct`）
- `AGENT_PEER_STREAM_TIMEOUT_SECONDS`：`agent_send_message` 拉取对端 SSE 输出的超时秒数（默认 `60`）
- `AGENT_PEER_BROADCAST_STREAM_TIMEOUT_SECONDS`：`agent_broadcast` 汇总多目标 SSE 输出的总超时秒数（默认 `20`，超时截断已收集内容）
