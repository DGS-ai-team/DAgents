# DAgents API 文档（与实现同步）

> **【已弃用 — Agent 运行时】** 本文档描述 **Python FastAPI** 契约（`app/harness/api`）。  
> 本地助手 Agent API 见 [architecture/agent-node-api.md](./architecture/agent-node-api.md)。  
> 弃用说明：`app/deprecated_backend.py`；`GET /health` 与响应头 `Deprecation: true`。

本文档与 **`v0.2.0`** 前后端行为对齐，面向前端接入与联调；实现以代码为准。

| 项目 | 说明 |
|------|------|
| 实现入口 | `app/harness/api/app.py`（`create_app()`，`FastAPI(..., version="0.2.0")`） |
| 进程入口 | 仓库根目录 `run_agent_api.py`（监听 **`API_HOST`** / **`API_PORT`**，默认 **`127.0.0.1:8000`**） |
| 编排 / SSE 映射 | `app/harness/service/agent_service.py`（**`_map_event_envelope_to_stream`**） |
| SSE 总线 | `app/harness/streaming/events.py`（**`InMemoryEventBus`**） |
| 队列优先级 | `app/harness/queue/message_queue.py`（**`MessagePriority`**） |

**入站/出站专题**（消息队列 + SSE 串联、联调要点）：[agent-input-output.md](./agent-input-output.md)。

**OpenAPI 单一来源**：运行时可访问 **`GET /openapi.json`**、**`GET /docs`**（Swagger UI）、**`GET /redoc`**；分仓导出见仓库根 **`export_openapi_schema.py`**。

**Go Agent Node**：本地助手运行时，见 [design/agent-client-refactor-plan.md](./design/agent-client-refactor-plan.md) 与 [architecture/agent-node-api.md](./architecture/agent-node-api.md)。下文 **§3.8 触发器** 等 Python 路径以 FastAPI 为准；Go triggers 见 [`node/internal/triggers/README.md`](../node/internal/triggers/README.md)。

**启动副作用（非 HTTP）**：若配置 **`REGISTRY_URL`**、**`AGENT_PUBLIC_BASE_URL`**、**`DISCOVERY_GROUPS`**、**`AGENT_ID`** 等完整，进程 **lifespan** 会向 Register Center **`POST /v1/agents`** 自登记，关闭时 **`DELETE /v1/agents/{agent_id}`**；缺省或网络失败仅打日志，**不**阻塞 API 监听。

---

## 1. 基本信息

- **URL 前缀**：业务路由在 **`/v1`**；健康检查 **`/health`** 无前缀。
- **认证**：当前 **无** HTTP 鉴权（部署侧请自行网络隔离或反向代理鉴权）。
- **CORS**：由 **`API_CORS_ALLOW_ORIGINS`**（逗号分隔）控制；配置含 **`*`** 时对浏览器来源等价全开放（仅建议本地调试）。未配置时应用内回退本地 Vite 常用来源（见 `app/config/settings.py`）。
- **指标**：**`GET /metrics`** 仅在 **`METRICS_ENABLED=true`**（默认）时注册；关闭后路由不存在（返回 **404**）。

---

## 2. 统一约定

- 普通 JSON 接口返回 **`application/json`**（FastAPI 默认）。
- **`GET /v1/streams`** 为 **SSE**（**`Content-Type: text/event-stream`**）。
- 提交消息接口 **不** 返回 `request_id`；前端按 **`session_id`** 与会话状态组织 UI。
- **SSE 与 `connection_id`**：
  - 流式连接 **必须** 带查询参数 **`connection_id`**（由 **`POST /v1/connections`** 创建）；缺失或非法时由框架返回 **422**。
  - **`POST /v1/messages`** 请求体 **必填** **`connection_id`**（`min_length=1`）；**若 `MessageEnvelope.connection_id` 为空/空白，服务层不会向总线推送 SSE**（与 `app.py` 中 `handle_stream_event` 一致）。
- **SSE 帧内 envelope**（`StreamEvent` 序列化）字段：
  - **`connection_id`**：string  
  - **`session_id`**：string  
  - **`type`**：string（与 SSE 的 `event:` 行一致）  
  - **`seq`**：number（按 `connection_id` 维度递增）  
  - **`ts`**：string（UTC ISO8601）  
  - **`data`**：object（扁平业务字段 + 内嵌 **`meta`**，见下文 §4）

---

## 3. HTTP 接口一览

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 存活探测 |
| `GET` | `/metrics` | Prometheus 文本（**可选**，见 §3.2） |
| `POST` | `/v1/connections` | **v2**：创建 Client connection（见 §3.10） |
| `POST` | `/v1/sessions` | 创建会话 |
| `POST` | `/v1/messages` | 提交用户消息或 resume |
| `GET` | `/v1/streams` | 全局 SSE（**必填** `connection_id`） |
| `POST` | `/v1/sessions/{session_id}/cancel` | 取消当前 turn |
| `POST` | `/v1/sessions/{session_id}/clear-context` | 清空对话上下文（保留技能与首条请求标识） |
| `GET` | `/v1/sessions/{session_id}/context` | 查询当前会话 context 摘要 |
| `GET` | `/v1/sessions/{session_id}/skills` | 查询当前会话 skills |
| `POST` | `/v1/sessions/{session_id}/skills/load` | 加载一个 skill |
| `POST` | `/v1/sessions/{session_id}/skills/unload` | 卸载一个 skill |
| `DELETE` | `/v1/sessions/{session_id}` | 释放会话资源并可选清库 |
| `POST` | `/v1/triggers` | 创建触发器资源 |
| `GET` | `/v1/triggers` | 列出触发器资源 |
| `GET` | `/v1/triggers/{trigger_id}` | 查看单个触发器 |
| `PATCH` | `/v1/triggers/{trigger_id}` | 更新触发器 |
| `DELETE` | `/v1/triggers/{trigger_id}` | 删除触发器 |
| `POST` | `/v1/triggers/{trigger_id}/fire` | 手动触发并投递任务 |
| `GET` | `/v1/triggers/{trigger_id}/history` | 查看触发历史 |
| `POST` | `/v1/bodies/register` | **v2**：Proxy 注册 Body（Bearer bootstrap；见 §3.9） |
| `GET` | `/v1/bodies/status` | **v2**：Body/Proxy 只读摘要（TUI 联调；见 §3.9） |
| `WS` | `/v1/bodies/control/{proxy_connection_id}` | **v2**：Proxy 出站控制通道（见 §3.9） |

---

### 3.1 健康检查

- **`GET /health`**
- 响应示例（含弃用元数据）：

```json
{
  "status": "ok",
  "deprecated": true,
  "replacement": "go-agent-node",
  "deprecation_notice": "【已弃用】Python FastAPI Agent 运行时…",
  "successor_doc": "docs/architecture/overview.md"
}
```

- 所有 API 响应另含 HTTP 头：**`Deprecation: true`**、**`X-DAgents-Backend: python-deprecated`**、**`X-DAgents-Successor: go-agent-node`**

---

### 3.2 Prometheus 指标（可选）

- **`GET /metrics`**
- **条件**：仅当 **`METRICS_ENABLED`** 为真（默认）时注册；否则无此路由。
- 响应：`text/plain`（Prometheus exposition 格式）。细节见 **`docs/prometheus-metrics.md`**。

---

### 3.3 创建会话

- **`POST /v1/sessions`**
- **请求体**（`SessionCreateIn`）：
  - **`session_id`**：`string | null`（可选；不传或空串时服务端生成 UUID）
  - **`connection_id`**：`string | null`（可选，Phase 2；创建时绑定 session 到 connection）
  - **`agent_id`**：`string | null`（可选；缺省 Backend **`AGENT_ID`**；与 `connection_id` 同时提供时必须一致）
- **响应体**（`SessionCreateResult`）：
  - **`session_id`**：`string`
  - **`agent_id`**：`string`
  - **`body_id`**：`string | null`（Proxy 在线时为 `body-{agent_id}`，否则 `null` 表示 backend-only 会话）
  - **`created`**：`bool`（当前实现固定为 **`true`**）
- **错误**：容量或内部错误 **400**；connection 无效 **404/410**；agent_id 与 connection 冲突 **409**。

**v1 兼容**：仅传 `session_id` 时行为不变；响应新增 `agent_id` / `body_id` 字段（旧 Client 可忽略）。

**示例**：

```json
{
  "session_id": "s-web",
  "agent_id": "ops-01",
  "body_id": "body-ops-01",
  "created": true
}
```

---

### 3.4 提交消息 / 恢复执行

- **`POST /v1/messages`**
- **请求体**（`MessageIn`）：
  - **`session_id`**：`string`，**必填**，`min_length=1`
  - **`connection_id`**：`string`，**必填**，`min_length=1`（须与 SSE 订阅使用同一值）
  - **`request_type`**：`"message" | "resume"`，默认 **`"message"`**
  - **`content`**：`string | null`；当 **`request_type === "message"`** 时 **必填且非空白**，否则 **422**
  - **`resume_value`**：`any | null`；**`resume`** 时由编排消费（审批结构见 **`app/schemas/approval.py`**）
  - **`source`**：`string`，默认 **`"api"`**
  - **`priority`**：`"tool_result" | "human" | "resume" | "other" | null`
    - **缺省规则**（服务端 `model_validator`）：  
      - **`message` → `human`**  
      - **`resume` → `resume`**  
    - **语义**：**`human` 仅影响入队优先级，不会自动调用取消接口**；打断在途 turn 请显式 **`POST .../cancel`**（见 `AgentService.submit_message` 注释）。
- **响应体**（`SubmitResult`）：
  - **`accepted`**：固定 **`true`**（异常路径不会返回本模型）
  - **`session_id`**：回显请求体
  - **`priority`**：实际入队优先级（缺省已填充后的值）
- **错误**：参数类 **422**；入队失败、会话上限等 **400**，`detail` 为字符串。

#### `message` 示例

```json
{
  "session_id": "s-web",
  "connection_id": "client-001",
  "request_type": "message",
  "content": "你好",
  "source": "client"
}
```

#### `resume` 示例（选择性审批）

```json
{
  "session_id": "s-web",
  "connection_id": "client-001",
  "request_type": "resume",
  "resume_value": {
    "type": "selection",
    "approved": ["tool-call-id-1"],
    "rejected": ["tool-call-id-2"]
  },
  "source": "client"
}
```

#### `resume_value` 支持结构（解析见 `parse_resume_tool_decision`）

- `{"type":"approve"}`
- `{"type":"reject"}`
- `{"type":"selection","approved":[...],"rejected":[...]}`
- 非法或缺省类型时运行时按 **reject** 语义处理（不向外抛校验错误）

> **说明**：HTTP 层 **`request_type`** 仅 **`message` / `resume`**。内部队列另有 **`async_tool_result` / `tool_result`** 等，由服务与异步工具仓写入，**不经** `POST /v1/messages`。其中 **`async_tool_result`** 入队的 **`MessageEnvelope.connection_id`** 与异步任务快照中的 **`connection_id`** 一致，用于 **SSE 分桶**（见 [agent-input-output.md](./agent-input-output.md)）。

---

### 3.5 全局 SSE（推荐前端单连接）

- **`GET /v1/streams?connection_id=<your_connection_id>`**
- **用途**：同一 **`connection_id`** 下跨 **`session_id`** 的实时事件流。
- **行为**：仅推送 **订阅建立之后** 的事件，**不**回放历史。
- **错误**：缺少 **`connection_id`** 时 **422**。

**帧格式**见 §4。

---

### 3.6 取消当前推理 turn

- **`POST /v1/sessions/{session_id}/cancel`**
- **路径参数**：**`session_id`**（纯空白时 **422**）
- **响应体**（`CancelTurnResult`）：
  - **`session_id`**：规范化后的 id
  - **`cancelled`**：`bool` — **无在途 `_handle_message` 时为 `false`**（幂等语义）

---

### 3.6.1 清空对话上下文

- **`POST /v1/sessions/{session_id}/clear-context`**
- **路径参数**：**`session_id`**（纯空白时 **422**）
- **用途**：在同一 session 下清空 `messages` / `pending_tool_calls` 等对话态并重置 `run_turn_phase`；**保留** `loaded_skills`、`sse_connection_id` 与 sqlite **`first_request_message`**（便于 `show session` 识别）。
- **行为**：
  - 若有在途 turn，会先 **cancel 并等待结束** 再清空（避免 `finally` 落盘覆盖清空结果）；
  - 取消该 session 的 summary 压缩后台 task；
  - **不**清空消息队列中已入队但未消费的消息（后续将在空 context 上继续处理）。
- **响应体**（`SessionClearContextResult`）：
  - **`session_id`**：string
  - **`cleared`**：`bool` — 成功时为 `true`
  - **`cancelled_turn`**：`bool` — 是否取消了在途 turn

**错误**：参数非法 **422**；内部失败 **400**。

---

### 3.6.2 会话 context 摘要

- **`GET /v1/sessions/{session_id}/context`**
- **路径参数**：**`session_id`**（纯空白时 **422**）
- **用途**：只读查询当前会话内存/持久化 context 摘要，供 TUI `/context` 视图展示。
- **响应体**（`SessionContextResult`）：
  - **`session_id`**：string
  - **`sse_connection_id`** / **`active_connection_id`**：当前 SSE/活跃 client 标识
  - **`run_turn_phase`**：当前 turn 阶段
  - **`messages_count`** / **`pending_tool_calls_count`** / **`messages_total_tokens`** / **`tool_loop_count`**：context 计数
  - **`loaded_skills`**：`[{ skill_name, description }]`
  - **`queue_pending`** / **`has_active_turn`**：队列与在途 turn 状态
  - **`recent_messages`**：最近若干条 OpenAI message 预览（角色、截断内容、tool call/reasoning 标记）

**错误**：参数非法 **422**；内部失败 **400**。

---

### 3.6.3 会话 skills

- **`GET /v1/sessions/{session_id}/skills`**
- **`POST /v1/sessions/{session_id}/skills/load`**
- **`POST /v1/sessions/{session_id}/skills/unload`**
- **路径参数**：**`session_id`**（纯空白时 **422**）
- **load/unload 请求体**：
  - **`skill_name`**：`string`（必填）
- **用途**：
  - 查询当前会话已加载的 `loaded_skills` 与磁盘启用的 `available_skills`；
  - `load` 向当前会话追加一个 enabled skill；
  - `unload` 从当前会话移除一个 skill。
- **响应体**（`SessionSkillsResult`）：
  - **`session_id`**：string
  - **`loaded_skills`**：`[{ skill_name, description }]`
  - **`available_skills`**：`[{ skill_name, description }]`

**错误**：参数非法、skill 不存在或超过加载上限时 **422**；内部失败 **400**。

---

### 3.7 释放会话

- **`DELETE /v1/sessions/{session_id}`**
- **路径参数**：**`session_id`**（纯空白时 **422**）
- **用途**：
  - 释放该会话在进程内的队列、消费者、上下文等；
  - **`clear_persisted=True`**（API 固定传入）且启用 **SQLite** 时，删除该会话持久化行。
- **响应体**（`SessionReleaseResult`）：
  - **`session_id`**：string
  - **`released`**：`bool`  
    - 若启用 sqlite 且执行了持久化清理分支，实现上在完成删库路径后返回 **`true`**；  
    - **未启用 sqlite** 时，表示此前该会话是否曾在**内存**中活跃过（**从未创建**则可能为 **`false`**）。调用方可将 **404/200** 均视为「已尽力释放」的幂等体验，具体以前端需求为准。

**错误**：内部失败（如删库异常）**400**，`detail` 为字符串。

---

### 3.8 触发器控制面

触发器用于在显式用户消息之外，按受控条件唤起 Agent 执行任务。第一版支持触发器资源管理、JSON 持久化、interval/once 调度、手动 fire 和触发历史；所有触发最终都会投递到现有 `AgentService.submit_message`，不会绕过工具审批与 session 队列。

- **存储路径**：`<运行根>/.runtime/triggers/triggers.json`
- **开关**：`TRIGGERS_ENABLED=true|false`；关闭后 API 仍可管理资源，但 `POST /fire` 返回 **503**。
- **调度轮询**：`TRIGGER_SCHEDULER_POLL_SECONDS`，默认 **5** 秒。

#### 创建触发器

- **`POST /v1/triggers`**
- 常用请求字段：
  - **`name`**：名称，必填。
  - **`condition`**（必填，不可为空）：`{"interval_seconds": 60}` 周期，或 `{"fire_at": 1730000000}` 单次；二者不可同时设置。
  - **`task_template`**：投递给 Agent 的任务模板，必填；支持 `{trigger_id}`、`{trigger_name}`、`{reason}`、`{payload_json}`。
  - **`target_session_id`** / **`connection_id`**：可选；HTTP 创建时可指定；未指定时调度器 fire 时自动创建 **`API_CLIENT`** connection；Agent 工具 `trigger_create` 会从当前会话 `context` 自动绑定。
  - 新建触发器默认 **`enabled=true`**，并根据 `condition` 计算 `next_fire_at`。

#### 手动触发

- **`POST /v1/triggers/{trigger_id}/fire`**
- 请求体：

```json
{
  "reason": "manual",
  "payload": {"service": "payment"},
  "force": true
}
```

响应为 `TriggerFireRecord`：包含 `status`（`queued | skipped | error`）、`session_id`、`connection_id`、最终投递 `content` 与可读 `message`。

#### 安全边界

- 触发器只负责唤起任务，不直接执行工具。
- 工具调用仍走现有 `approval_required` / `resume` 流程。
- 调度器对 `enabled=true` 且 `next_fire_at` 到期的触发器自动 fire；工具/API 写入类操作仍受工具审批策略约束。
- 触发历史会记录来源、payload、投递内容、结果和错误信息，作为后续 Audit Timeline 的数据基础。

---

### 3.9 v2 Body 注册与控制通道（已废弃）

> **状态（2026-05）**：本节描述已回退的 Brain/Body + Proxy 方案，**现网无对应实现**。当前架构见 [architecture/overview.md](./architecture/overview.md)。

**前置配置**（`.env` / 环境变量）：

| 变量 | 说明 |
|------|------|
| `V2_PROXY_BOOTSTRAP_TOKEN` | Proxy 注册时 Bearer token；**未配置时** `POST /v1/bodies/register` 返回 **503** |
| `V2_BACKEND_INSTANCE_ID` | Backend 实例标识（默认 `backend-local`） |
| `V2_PROXY_HEARTBEAT_TIMEOUT_SECONDS` | 心跳超时 sweep（默认 **90**） |
| `V2_BODY_ASYNC_ENABLED` | body 长时 bash 走 async detached（默认 **true**） |
| `V2_BODY_ASYNC_TIMEOUT_THRESHOLD_SECONDS` | 超过该秒数的 `bash_run` 使用 async（默认 **30**） |
| `V2_ALLOW_LOCAL_BODY_FALLBACK` | Proxy 不可达时 body 工具降级本地执行（默认 **true**） |

#### Body 注册

- **`POST /v1/bodies/register`**
- **认证**：**`Authorization: Bearer <V2_PROXY_BOOTSTRAP_TOKEN>`**（缺失 **401**；不匹配 **403**）
- **请求体**（`RegisterBodyRequest`）：
  - **`desired_agent_id`**：`string | null`（可选；缺省 Backend **`AGENT_ID`** / `settings.agent_id`）
  - **`requested_template_id`**：`string | null`（可选；内建 AgentTemplate ID，见下表；未知 ID **400**）
  - **`body_manifest`**：`object`
    - **`capabilities`**：`string[]`（默认 `[]`）
    - **`host_info`**：`object`（默认 `{}`）
    - **`local_constraints`**：`object`（默认 `{}`）
- **响应体**（`RegisterBodyResponse`）：
  - **`agent_id`**：`string`
  - **`body_id`**：`string`（Phase 1 形如 `body-{agent_id}`）
  - **`proxy_connection_id`**：`string`（形如 `pconn-...`）
  - **`control_channel_url`**：`string`（相对路径，如 `/v1/bodies/control/pconn-...`）
  - **`accepted_manifest_version`**：`number`
  - **`agent_status`**：`string`（如 **`active`**、`pending_approval`（`ops-terminal-agent` 模板））
- **内建 AgentTemplate**（`requested_template_id`）：

| template_id | 用途 | 默认 `agent_status` | 要求 capabilities |
|-------------|------|---------------------|-------------------|
| `personal-terminal-agent` | 用户终端/工作站 Body | `active` | `shell`, `filesystem` |
| `ops-terminal-agent` | 运维宿主机 Body | `pending_approval` | `shell` |
| `backend-helper-agent` | 偏 Backend 工具的辅助 Agent | `active` | （无） |

- **行为**：
  - 提供 **`requested_template_id`** 时按模板 upsert 内存 **`AgentRecord`**（`app/harness/agents/v2/`），并校验 manifest capabilities；
  - 同一 body 重复注册会 **替换** active 连接（旧连接标记 offline）；
  - Phase 2 **不**同步 Register Center（RC 同步留 Phase 3）。

请求示例：

```json
{
  "desired_agent_id": "k8s-ops-01",
  "body_manifest": {
    "capabilities": ["shell", "filesystem"],
    "host_info": { "os": "linux", "arch": "amd64" },
    "local_constraints": { "fs_root": "/opt/dagents/workspace" }
  }
}
```

#### 控制通道 WebSocket

- **`WS /v1/bodies/control/{proxy_connection_id}`**
- **调用方**：Go Proxy（**出站**连接 Backend；Backend 无需直连宿主机）
- **协议**：JSON envelope（`execute` / `result` / `heartbeat` 等）；原设计文档已移除，见 [agent-client-refactor-plan.md](./agent-client-refactor-plan.md)
- **边界**：
  - 未知或已 offline 的 `proxy_connection_id` 会被拒绝（关闭码 **4404** / **4403**）；
  - Backend 经 hub 下发 body 工具执行；Proxy 返回 `result` 后 Execution Dispatcher 完成记录。

**Go Proxy 联机**（同机示例）：

```bash
# Backend 已启动且 V2_PROXY_BOOTSTRAP_TOKEN 已配置
dagents-proxy -connect -backend http://127.0.0.1:8000 -bootstrap-token "$V2_PROXY_BOOTSTRAP_TOKEN"
```

实现入口：`app/harness/api/v2/bodies.py`、`app/proxy/v2/control_channel.py`；Go 客户端见 `proxy/v2/internal/client/`。

#### v1 工具名 → Proxy canonical（Phase 2 S7）

LLM / Tool Manifest 仍使用 **v1 名**；Execution Dispatcher 经 `body_executor` 映射后下发：

| v1（LLM 可见） | Proxy `execute.tool` | 本地降级（`V2_ALLOW_LOCAL_BODY_FALLBACK`） |
|----------------|------------------------|--------------------------------------------|
| `bash_run` | `shell_exec` | 是 |
| `read_file` | `file_read` | 是 |
| `write_file` | `file_write` | 是 |
| `search_file` | `file_search` | 否（需在线 Proxy） |
| `bash_job_status` | `shell_job_status` | 否 |
| `bash_job_tail` | `shell_job_tail` | 否 |
| `bash_job_cancel` | `shell_job_cancel` | 否 |

**Body offline 可见性**：无 active control channel 时，`build_openai_toolkit()` 的 **tools payload** 会隐藏上表中不可降级的 body 工具；执行层 `tool_map` 仍保留全量名称以便一致报错。实现：`app/harness/tools/v2/visibility.py`。

#### 临时子 Agent（Phase 2 S8）

- **工具名**：`create_temporary_agent`（`tool.kind=backend`）
- **body_binding**：`backend_only`（无 Body）或 `inherit_parent_body`（共享父 session 的 `body_id`）
- **模板**：`code-review-helper`、`research-helper`、`ops-check-helper`（见 `app/config/v2/temporary_agent_template.py`）
- **配置**：`V2_DEFAULT_CHILD_AGENT_TTL_SECONDS`（默认 **1800**）、`V2_MAX_CHILD_AGENT_TTL_SECONDS`（**7200**）、`V2_DEFAULT_CHILD_AGENT_MAX_TURNS` / `V2_MAX_CHILD_AGENT_TURNS`
- **生命周期**：TTL 到期自动清理；`DELETE /v1/sessions/{id}` 取消仍 active 的子 Agent；默认 **不** 进入 RC discover

#### Body / Proxy 只读状态（TUI 联调）

- **`GET /v1/bodies/status`**
- **认证**：无（与 v1 API 一致）
- **响应体**（`BodiesStatusResponse`）：
  - **`v2_allow_local_body_fallback`**：Proxy 离线时 body 工具是否本地降级
  - **`proxy_bootstrap_configured`**：是否配置了 `V2_PROXY_BOOTSTRAP_TOKEN`
  - **`backend_instance_id`** / **`agent_id`**
  - **`bodies`**：数组，每项含 **`body_id`**、**`status`**、**`control_channel_ws_connected`** 等
- **用途**：Textual TUI 欢迎页与 `/status` 命令；`run_phase1_stack.py` 等待 Proxy WS 就绪

**一键 Phase 1 联调**：

```bash
python run_phase1_stack.py --chat
python run_v2_stack.py --chat
```

**Phase 2 联调（TUI 默认 v2 connection）**：

```bash
python run_v2_stack.py --chat
```

---

### 3.10 v2 Connection（Client Plane）

- **`POST /v1/connections`**
- **请求体**：
  - **`agent_id`**：`string | null`（可选；缺省 Backend **`AGENT_ID`**）
  - **`conn_type`**：`user_tui` | `web_ui` | `api_client` | `a2a_caller` | `a2a_callee` | `proxy`（默认 **`user_tui`**）
- **响应体**：
  - **`connection_id`**：`string`（形如 `conn-...`，同时作为 SSE 分桶 ID）
  - **`agent_id`** / **`conn_type`**
  - **`expires_at`**：Unix 时间戳（默认 TTL **`V2_CONNECTION_TTL_SECONDS`**，86400）

**v2 Client 典型流程**：

```text
POST /v1/connections  →  connection_id
POST /v1/sessions     →  session_id（S3 起可带 connection_id）
GET  /v1/streams?connection_id=...
POST /v1/messages     { connection_id, session_id, ... }
```

**`GET /v1/streams`**：**必填** `connection_id`（须为有效、未过期的 connection）。

**`POST /v1/messages`**：**必填** **`connection_id`**；自动将 `session_id` 绑定到该 connection。

SSE JSON 帧携带顶层 **`connection_id`**、**`agent_id`**、**`body_id`**、**`execution_id`**（后三者来自 Session 绑定与 Execution 记录）。

---

## 4. SSE 事件格式

单条帧：

```text
event: <event_type>
data: {"connection_id":"...","session_id":"...","connection_id":"...","agent_id":"...","body_id":"...","execution_id":"...","type":"<event_type>","seq":0,"ts":"...","data":{...}}

```

- **`event:`** 与 JSON 内 **`type`** 一致（来自 **`StreamEvent.type`**）。
- **`data`** 为 `StreamEvent` 整包 JSON；其中 **`data.data`**（命名嵌套）为业务扁平字段：**`AgentService._map_event_envelope_to_stream`** 的输出对象（并含 **`meta`**）。

### 4.1 `data` 内常见 `type` 与扁平字段（`_map_event_envelope_to_stream`）

以下 **`display_type` 缺省值**以映射代码为准（编排层可在 payload 中显式覆盖）：

- **`assistant`**
  - **`content`**：`string`
  - **`display_type`**：默认 **`markdown`**
- **`reasoning`**
  - **`content`**：`string`
  - **`display_type`**：默认 **`reasoning`**
- **`usage`**
  - **`prompt_tokens`** / **`completion_tokens`**：number  
  - **`total_tokens`**：`number | null`  
  - **`prompt_audio_tokens`**、**`prompt_cached_tokens`**、**`prompt_cache_hit_tokens`**、**`prompt_cache_miss_tokens`**：number（缺省 **0**）
- **`tool_call_delta`**
  - **`tool_calls`**：`array`（OpenAI 流式分片，含 **`index`**；**执行与审批以回合末 `tool_call` 为准**）
- **`tool_call`**
  - **`assistant_content`**：`string`
  - **`tool_calls`**：`array`
  - **`display_type`**：默认 **`normal_text`**
- **`tool_result`**
  - **`content`**：`string`
  - **`tool_call_id`** / **`tool_name`**：`string | null`
  - **`display_type`**：默认 **`normal_text`**
  - **`rejected`** / **`interrupted_by_user_message`** / **`partial`**：`bool`
  - **`execution_id`**：`string | null`（v2；HITL 关联，亦见顶层 **`StreamEvent.execution_id`**）
- **`approval_required`**
  - **`approval_type`**：默认 **`execute_tool`**
  - **`content`**：string（来自 payload 的 **`message`** 字段）
  - **`approval_args`**：object（来自 payload 的 **`args`**）
  - **`description`**：string
  - **`approval_id`**：`string | null`
  - **`display_type`**：默认 **`normal_text`**
- **`error`**
  - **`message`**：`string`
- **`done`**
  - **`finish_reason`**：`string`（映射层保证存在；如 **`stop`**、**`tool_calls`**、**`error`**、**`resume_rejected`** 等）
  - 其余字段与 **`meta`** 合并展示
- **`chunk`**（兜底）
  - **`raw`**：未专门映射的 payload

### 4.2 `meta` 字段

各事件的 **`data.meta`** 由 **`base_meta`**（如 **`session_id`**、**`model`**）与事件 **`envelope.meta`** 合并得到。v2 路径下 API 层还会写入 **`agent_id`**、**`body_id`**、**`connection_id`**、**`execution_id`**（与 `StreamEvent` 顶层字段一致，便于嵌套 `data.data` 消费者读取）。

---

## 5. 错误语义（HTTP）

| 场景 | 常见状态码 |
|------|------------|
| Pydantic / 参数校验失败（如 **`message` 缺 `content`**、**`/streams` 缺 `connection_id`**） | **422** |
| 业务/容量/队列等失败（`HTTPException` 或编排外异常经服务转换） | **400**，`detail` 为字符串 |
| **`GET /metrics`** 未注册（指标关闭） | **404** |

---

## 6. 消息队列优先级（与 `POST /v1/messages` 的 `priority` 对应）

数值越小越优先（内部整型，HTTP 仍传字面量）：

| 字面量 | 内部值 | 说明 |
|--------|--------|------|
| **`tool_result`** | -1 | 工具结果回灌（HTTP 不直接提交此类型时，服务内部异步路径仍可能使用） |
| **`human`** | 0 | 用户主输入（**默认 `message` 使用**） |
| **`resume`** | 1 | 恢复 / 审批决策 |
| **`other`** | 10 | 其它（一般非 HTTP 默认） |

**同优先级 FIFO**：同一字面量下按入队顺序稳定出队（见 **`MessageQueue`**）。

---

## 7. 联调建议

1. **`POST /v1/connections`** 获取并长期持有 **`connection_id`**，建立 **`GET /v1/streams?connection_id=...`** 单连接。  
2. **`POST /v1/messages`** 使用 **同一 `connection_id`**，否则可能 **收不到 SSE**。  
3. 按 SSE 包内 **`session_id`** 在前端分流会话 UI。  
4. 排障顺序：**`/health`** → **`/streams`** 是否建立 → **`/v1/messages`** 与事件是否闭环到 **`done`**。

---

## 8. 环境变量（与 API 进程直接相关）

| 变量 | 说明 |
|------|------|
| **`API_HOST`** / **`API_PORT`** | 监听地址与端口（**`run_agent_api.py`**） |
| **`API_CORS_ALLOW_ORIGINS`** | CORS 来源列表（**`Settings.api_cors_allow_origins`**） |
| **`METRICS_ENABLED`** | 是否注册 **`/metrics`** |
| **`REGISTRY_URL`**、**`AGENT_PUBLIC_BASE_URL`**、**`DISCOVERY_GROUPS`**、**`AGENT_ID`** 等 | Register Center 自登记（**lifespan**；不完整则跳过） |

更全变量见仓库根 **`.env.example`** 与 **`app/config/settings.py`**。
