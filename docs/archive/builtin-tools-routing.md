# 内置工具与 `tool.kind` 路由

本文列出 **当前 v1 内置工具** 在 architecture-v2 下的 **`tool.kind` 归属**、执行位置与 Phase 1 落地范围。工具注册来源见 [built-in-tools.md](../built-in-tools.md)。

## 1. 路由规则摘要

```text
tool.kind == backend  → Backend executor（Python）
tool.kind == body     → 绑定 Body 的 Go Proxy（control channel）
```

特殊路径（不经 Execution Dispatcher 标准路径）：

- **`ask_user_information`**：Brain 编排器 HITL，见 [client-events-and-hitl.md](./client-events-and-hitl.md)。
- **`create_temporary_agent`**（v2 新增）：Backend Control Plane 创建子 Agent。

Body 工具执行前提：

- Agent 有 active Body Binding，且 `proxy_connection_id` online。
- 否则按 [agent-dual-runtime.md](./agent-dual-runtime.md) §10 降级或拒绝。

## 2. 完整对照表

| 工具名 | v2 `tool.kind` | 执行面 | 异步 | Phase 1 | 说明 |
|--------|----------------|--------|------|---------|------|
| `ask_user_information` | `backend` | 编排器 HITL | 否 | 保留 | 不下发 Proxy |
| `load_skills` | `backend` | Backend | 否 | 保留 | 读写 session `loaded_skills` |
| `unload_skills` | `backend` | Backend | 否 | 保留 | 空 `skill_names` 清空全部 |
| `read_file` | `body` | Proxy | 否 | Proxy 实现 `file_read` | 无 Body 时可降级 backend（兼容） |
| `search_file` | `body` | Proxy | 否 | Phase 2+ 或 backend 保留 | 依赖 fs |
| `search_replace` | `body` | Proxy | 否 | Phase 2+ | 写操作，须审批 |
| `write_file` | `body` | Proxy | 否 | Proxy 实现 `file_write` | |
| `bash_run` | `body` | Proxy | 可 | Proxy 实现 `shell_exec` | v1 超时转 async 见 control channel §7 |
| `bash_job_status` | `body` | Proxy | 否 | Phase 2+ | 异步 shell 配套 |
| `bash_job_tail` | `body` | Proxy | 否 | Phase 2+ | |
| `bash_job_cancel` | `body` | Proxy | 否 | Phase 2+ | |
| `trigger_list` | `backend` | Backend | 否 | 保留 | 读 `.runtime/triggers` |
| `trigger_get` | `backend` | Backend | 否 | 保留 | |
| `trigger_create` | `backend` | Backend | 否 | 保留 | 默认审批 |
| `trigger_update` | `backend` | Backend | 否 | 保留 | |
| `trigger_delete` | `backend` | Backend | 否 | 保留 | |
| `trigger_fire` | `backend` | Backend | 是 | 保留 | 投递 MessageQueue |
| `agent_discover` | `backend` | Backend | 否 | 保留 | 调 Register Center |
| `agent_send_message` | `backend` | Backend | 是 | 保留 | A2A |
| `agent_broadcast` | `backend` | Backend | 是 | 保留 | |
| `agent_peer_approve_tools` | `backend` | Backend | 是 | 保留 | 对端 HITL |
| `async_tool_status` | `backend` | Backend | 否 | 保留 | 查 AsyncToolResultStore |
| `async_tool_cancel` | `backend` | Backend | 否 | 保留 | |
| `create_temporary_agent` | `backend` | Control Plane | 否 | v2 新增 | 见 temporary-child-agents |
| `host_platform` | `body` 或 `backend` | 视部署 | 否 | 未注册 | 代码存在但未进 `get_tools()` |

## 3. v2 Canonical 工具名（Proxy 侧）

Phase 1 Proxy 可实现 v2 名称，Backend 做别名：

| v2（Proxy） | v1（Backend 别名） |
|-------------|-------------------|
| `shell_exec` | `bash_run` |
| `file_read` | `read_file` |
| `file_write` | `write_file` |

Tool Manifest 对外暴露给 LLM 的名称在迁移期可 **继续用 v1 名**，避免重训 prompt；路由层映射到 Proxy tool 实现。

## 4. 按能力分类

### 4.1 纯 Backend（无 Body 也可运行）

- Skills：`load_skills`、`unload_skills`（空数组清空全部）
- Triggers：全套 trigger 工具
- A2A：`agent_*`
- 异步仓管理：`async_tool_status`、`async_tool_cancel`
- HITL：`ask_user_information`
- 子 Agent：`create_temporary_agent`

### 4.2 依赖宿主机（Body）

- Shell：`bash_run` → `shell_exec`
- 文件：`read_file`、`write_file`、`search_file`、`search_replace`
- 环境探测：`host_platform`（若启用）

### 4.3 混合 Agent 典型 Manifest

运维 Agent（有 Proxy）：

```json
[
  {"name": "agent_discover", "kind": "backend"},
  {"name": "load_skills", "kind": "backend"},
  {"name": "bash_run", "kind": "body"},
  {"name": "read_file", "kind": "body"},
  {"name": "write_file", "kind": "body"},
  {"name": "ask_user_information", "kind": "backend"}
]
```

纯文档/推理 Agent（backend-only）：

```json
[
  {"name": "load_skills", "kind": "backend"},
  {"name": "agent_discover", "kind": "backend"},
  {"name": "ask_user_information", "kind": "backend"}
]
```

## 5. 审批默认值（与 v1 对齐）

Body 工具默认更严格；具体以 `decide_tool_approval` 与 `.runtime/policy/` 为准。v2 策略输入增加 `tool_kind`、`body_id`：

| 工具类 | 默认倾向 |
|--------|----------|
| body 读 | `auto` 或 `require_approval`（目录取决） |
| body 写 / shell 变更 | `require_approval` |
| backend trigger 写 | `require_approval` |
| `ask_user_information` | 免审批（编排器路径） |
| A2A 触发的 body 写 | `require_approval` |

## 6. 上下文压缩（Summary 子 Agent）

v1 在 Backend 内通过 `summary_compression` 触发压缩子流程，**不暴露为独立 tool**。

v2 归属：

- **Brain / Backend** 内部行为，`tool.kind` 不适用。
- 可选演进：改用 `create_temporary_agent` + `backend_only` 模板（Phase 2+），与 [temporary-child-agents.md](./temporary-child-agents.md) 统一。

## 7. Tool Manifest 与模型可见性

- LLM 看到的 tools 列表来自 Agent 的 **Tool Manifest 子集**（`get_tools()` 的全局池 ∩ Agent 授权）。
- `tool.kind` **不**出现在 OpenAI schema 中；仅 Control Plane 路由使用。
- Body offline 时，应从 manifest 动态 **隐藏** 或 **标记不可用** body 工具（避免模型反复调用失败）；backend-only 工具始终可见。
- **Phase 2 S7 落地**：`build_openai_toolkit()` 的 **LLM payload** 经 `filter_tools_for_v2_body_availability` 过滤；**执行层 `tool_map` 仍保留全量** body 工具以便 offline 报错一致。Proxy 在线时恢复全部 body 工具可见性。

## 8. Phase 1 实现检查清单

- [x] `ToolRef.kind` 数据模型与 Agent 配置
- [x] Execution Dispatcher 按 kind 分支
- [x] Proxy 实现 `shell_exec`、`file_read`、`file_write`
- [x] v1 名 → Proxy 名别名层（`body_executor`：`bash_run`/`read_file`/`write_file`/`search_file`/`bash_job_*`）
- [x] `allow_local_body_fallback` 配置（无 Proxy 时走 Backend fs/bash）
- [x] `ask_user_information` 保持编排器特殊路径
- [x] Body offline 时 LLM tools payload 隐藏不可降级 body 工具（S7 `visibility.py`）
- [x] 异步：`bash_run` 长任务 → control channel async（S6）；`bash_job_*` → Proxy `shell_job_*`（S7）
- [x] `create_temporary_agent` Backend 工具与 TTL 清理（S8）
