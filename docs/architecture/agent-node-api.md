# Agent Node HTTP API

本文描述 **Agent Node（Go）** 对外 HTTP/SSE 接口，与 `node/internal/api/` 对齐维护。
最小 OpenAPI：[`openapi-node.yaml`](./openapi-node.yaml)。Manage API 见 [manage-architecture.md](../design/manage-architecture.md)。

## 0. 契约要点

| 主题 | 说明 |
|------|------|
| **用户面主键** | **Agent**（`agent_id`）。1 Agent = 1 主对话；实现里的 session 仅内部结构 |
| **主路径** | `/v1/agents/{agent_id}/...`（ensure / hydrate / context / cancel / skills / child-agents / media / **policy** / **prompt-context**） |
| **策略与侧车** | 按 Agent 存 `agents.db`（`agent_policy` / `agent_prompt_context`）；侧车开关在 `config_snapshot_json.defaults.prompt_context` |
| **Manage 注册** | 载荷主字段为 `node_id`（若仍带 `agent_id`，值同 `node_id`）；`manage.enabled` 默认关 |
| **LLM 绑定** | 写在该 Agent 快照 `defaults.llm.active`，按 Agent 隔离 |

## 1. 设计原则

| 原则 | 说明 |
|------|------|
| **一进程多 Agent** | Node 进程持有多个 Agent 实例；对外按 `agent_id` 寻址 |
| **默认本机绑定** | 默认 `127.0.0.1`；人机为 Web UI `/ui/` |
| **思考与工具在 Node 内** | 无「Backend 代执行」路径；tool call 由 turn loop 本地完成 |
| **跨 Node 协作** | 经 Manage **Workgroup**（Dialer / 反代） |
| **工具边界** | 工具组 + policy + Agent `workspace_root`；Node 管理目录使用 `runtime_root`，无独立沙箱进程 |
| **会话态在 Node** | Agent 对话上下文、队列、持久化由 Node 负责（SQLite） |

### 1.1 基础路径

| 前缀 | 调用方 | 说明 |
|------|--------|------|
| `/ui/` | 浏览器 | 内嵌 Web UI |
| `/v1/agents/...` | Web UI / 本机客户端 | **主契约**：对话、策略、侧车、子 Agent |
| `/v1/workgroups/...` | Web UI（反代 Manage） | 工作组 |
| `/v1/...` | 本机 | messages、streams、triggers、setup、llm |
| `/health` | 探活 | 运维脚本 |

### 1.2 通用错误体

```json
{
  "error": {
    "code": "agent_not_found",
    "message": "agent 不存在",
    "details": { "agent_id": "agt-xxx" }
  }
}
```

常见 `code`：`invalid_agent`、`agent_not_found`、`turn_busy`、`policy_denied`、`approval_required`、`llm_error`、`tool_error`。

### 1.3 认证（Phase 递进）

| 调用方 | Phase 1 | Phase 2+ |
|--------|---------|----------|
| Client（本地） | 可选 `127.0.0.1` 免鉴权；或 `Authorization: Bearer <local_client_token>` | mTLS / Unix socket |

**无** 其他 Agent Node 入站认证（peer 不直连本 Node）。

---

## 2. Client Plane（本地）

### 2.1 健康与元数据

```http
GET /health
```

```json
{ "status": "ok", "agent_id": "ops-win-01", "version": "0.5.1" }
```

`version` 字段为全项目唯一语义化版本（权威来源：根目录 `VERSION`；Release 构建注入 `node/internal/version.Version`）。探活中的身份字段以 **`node_id`** 为准。

```http
GET /v1/agent/info
```

```json
{
  "agent_id": "ops-win-01",
  "capabilities": ["shell", "filesystem", "skills"],
  "manage_registered": true
}
```

### 2.2 Agents（主契约）

```http
POST /v1/agents
Content-Type: application/json

{
  "display_name": "助手",
  "defaults": { "llm": { "active": "deepseek", "max_tool_loops": 32 } }
}
```

创建时会种子写入该 Agent 的 **policy** 与 **prompt-context**（SQLite）。

```http
POST /v1/agents/{agent_id}/ensure
GET  /v1/agents/{agent_id}/hydrate
POST /v1/messages
Content-Type: application/json

{ "agent_id": "agt-xxx", "content": "你好" }
```

### 2.3 Policy（按 Agent / SQLite）

```http
GET /v1/agents/{agent_id}/policy
PUT /v1/agents/{agent_id}/policy/tools
PUT /v1/agents/{agent_id}/policy/shell/{bash|cmd|powershell}
```

全局 `GET/PUT /v1/policy*` **已移除**（404）；请用上表按 Agent 路径。

### 2.4 侧车正文（按 Agent / SQLite）

```http
GET /v1/agents/{agent_id}/prompt-context
PUT /v1/agents/{agent_id}/prompt-context
Content-Type: application/json

{
  "soul_md": "...",
  "user_md": "...",
  "custom_md": "...",
  "long_term_md": "..."
}
```

注入开关仍通过 Agent 快照 `defaults.prompt_context.*_enabled`（设置页「侧车与长期记忆」）。

### 2.5 消息与 resume

```http
POST /v1/messages
Content-Type: application/json

{
  "agent_id": "agt-xxx",
  "request_type": "message",
  "content": "列出当前目录"
}
```

`request_type`：`message` | `resume`。

resume 示例（审批，与 `node/internal/hitl/resume.go` 一致）：

```json
{
  "agent_id": "agt-xxx",
  "request_type": "resume",
  "resume_value": {
    "type": "selection",
    "approved": ["call_abc123"],
    "rejected": []
  }
}
```

用户询问示例：

```json
{
  "agent_id": "agt-xxx",
  "request_type": "resume",
  "resume_value": {
    "type": "user_information",
    "tool_call_id": "call_abc123",
    "answer": "Go",
    "selected_options": ["go"]
  }
}
```

启用子 Agent 时，父 session 的 resume 经 `RouteResume` → `DeliverParentResume` **只入队一次**（`node/internal/session/manager.go`）。

响应：

```json
{ "accepted": true, "agent_id": "agt-xxx", "priority": "human" }
```

### 2.4 SSE 事件流

```http
GET /v1/streams?agent_id=agt-xxx&live=1
Accept: text/event-stream

# reconnect
GET /v1/streams?agent_id=agt-xxx&after_agent_seq=42
```

- `live=1` 首连由 Hub 原子确定订阅点；Agent 过滤流重连使用 `after_agent_seq`。
- `seq` 是进程级诊断序号；`agent_seq` 是当前 Agent 的可重放连续游标。Node 重启通过 `stream_epoch` 区分新旧事件纪元。
- 历史被截断时发送 `resync_required`，Client 必须 hydrate 后继续，不得静默跳过缺失事件。
- 帧格式见 [附录/SSE事件速查.md](../handbook/附录/SSE事件速查.md)。

核心事件：`assistant`、`reasoning`、`tool_call`、`tool_result`、`turn_state`、`hitl_required`、`turn_finished`、`side_effect_turn_start`、`side_effect_applied`、`side_effects_cleared`、`temporary_agent_created` / `temporary_agent_completed` / `temporary_agent_cancelled`、`error`、`resync_required`。

**本地 turn** 统一使用 `hitl_required`。子 Agent 相关路径仍可能出现 `approval_required` / `user_information_required`，UI 按同类 HITL 处理即可。

#### 2.4.1 `turn_finished` 与 `turn_state`

`turn_state` 是 Turn Coordinator 的生命周期唯一权威，包含当前 turn/step、phase、工具执行态、交互态和终态。`hitl_required` 是需要用户交互的事实事件；`turn_state.phase=tool_waiting|waiting_user` 是暂停状态。

`turn_finished` 只表示当前 turn 已真正进入终态，不能表示 HITL 暂停。实现：`node/internal/turn/sse_publish.go` 中 `publishTurnFinished`。

**载荷字段**

| 字段 | 类型 | 说明 |
|------|------|------|
| `finish_reason` | string | `stop` \| `error` \| `cancelled` 等终态原因 |
| `turn_complete` | bool | 固定为 `true`；事件只在 turn 终态发送 |
| `tool_context_metrics` | object | 本 turn 工具链观测指标（可选） |

**何时发送 `turn_finished`**

| 场景 | 发送 |
|------|------|
| 模型一步结束且无 `tool_calls` | ✅ |
| `ask_user_information` / 审批 pending | ❌，仅发 `hitl_required` + 等待态 `turn_state` |
| LLM/工具错误、取消、超循环上限 | ✅ |
| 自动工具执行后 `tool_result` 续跑 | ❌ |
| 客户端 `resume` 刚处理完、链继续 | ❌，最终终态时再发 |

一次 submit 可经历多次 `hitl_required` / resume，但只在整个 turn 终态时发送一次 `turn_finished`。

**与其它事件分工**

- **`hitl_required`**：本地 turn 统一 HITL 事件；`items[]` 每项含 `hitl_type`：`user_information`（`ask_user_information`）或 `execute_tool`（需审批工具）。UI 按 item 类型展示并分别 `POST resume`；同批可混合 ask + approval，Node 侧为单一 `PendingHITL.Items`。
- **`approval_required` / `user_information_required`**：子 Agent 等路径仍可能使用；UI 按同类 HITL 处理。
- `tool_call`（含 `ask_user_information`）：工具行展示；**不**替代 HITL 块。
- 子 Agent 内部 `turn_finished`：**不**转发为父 Agent 终态（`node/internal/childagent/relay_hub.go`）。

**Web UI 等交互客户端**：收到 `hitl_required` 后展开为 HITL 队列（先 user_information item，再 execute_tool 合并为 approval 面板）；每步 resume 后 Node 可部分消 pending，全部 resolved 才续跑 tool loop。等待审批期间不能因为普通 human message 清除卡片；只有显式 cancel 或 resume 才改变该 turn。

#### 2.4.2 `hitl_required` 载荷（本地 turn）

| 字段 | 说明 |
|------|------|
| `hitl_id` | 批次 id |
| `message` | 摘要文案 |
| `items[]` | 待交互项；每项含 `hitl_type` |

| `items[].hitl_type` | 额外字段 | Client resume |
|---------------------|----------|---------------|
| `user_information` | `content`、`user_information_args`（含 `tool_call_id`） | `type=user_information` |
| `execute_tool` | `id`、`name`、`arguments`、`approval_reason`、`risk_level`、… | `type=approval` / `selection` |

实现：`turn/hitl_payload.go` → `publishHITLRequired`（`sse_publish.go`）。

#### 2.4.3 旁路 side-effect 事件（Produce / 被动续跑）

async tool result 在 **任务未完成**（HITL、open batch、tool loop）时 **Produce**：立即 SSE、写入 `sideEffectStore`，**不改** `runtime.messages`、**不**跑 LLM。Apply 在 `runTurnStep` 步首；被动续跑经内部队列 `side_effect_continue`。

| 事件 | 何时发送 | 典型 `data` | Client 行为 |
|------|----------|-------------|-------------|
| `side_effect_turn_start` | `handleSideEffectContinue` **跑 LLM 之前** | `source`（`side_effect_continue` \| `cancel_recovery` \| `task_complete_produce`）、`side_effect_pending`、`implicit_turn: true` | **`beginImplicitTurn` / `beginSubmit`**，进入被动等待态 |
| `side_effect_applied` | `ApplyReady` 成功写入 history 后 | `seqs: number[]` | 将 `side_effect_seq` 匹配的 callback 行标为 **已入库** |
| `side_effects_cleared` | `ClearContext` / `Delete` 丢弃 server 缓冲 | `dropped`、`seqs: number[]` | 将未入库条目标为 **已失效** |
| `tool_call` / `tool_result`（Produce） | async tool result Produce | 与正常工具事件相同，可含 `deferred: true`、`side_effect_seq` | idle 时仍渲染；**不**因 Produce 单独 `finishTurn` |

**Produce vs Apply**

| 时刻 | Server history | Client transcript |
|------|----------------|-------------------|
| Produce ×N | 缓冲 | N 条 callback 工具行（SSE 已发） |
| Apply（步首，可合并 `get_callback`） | 写入 1 条或多条 | **`side_effect_applied`**（无新 callback SSE） |
| Continue LLM | 正常 assistant 流 | 流式 + `turn_finished` |

**`implicit_turn` 语义**：**非**用户 `POST /v1/messages` 驱动的 turn。Client 收到 `side_effect_turn_start` 后应开启与 user submit 相同的 turn 栅栏（Go `TurnGate.BeginImplicitTurn`、Web `beginImplicitTurn`、Python `begin_implicit_turn`），以便接收后续 `assistant` / `turn_finished`。

**Cancel 与缓冲**（`POST /v1/agents/{id}/cancel`）：

| 条件 | 行为 |
|------|------|
| 在途 LLM（`state != idle`） | `turnCtx` cancel → `turn_finished(cancelled)` |
| 缓冲非空 **且** `pending == nil` | 额外 `scheduleSideEffectContinue("cancel_recovery")` → `side_effect_turn_start` → Apply + LLM |
| 缓冲空 | 仅 abort，**不** schedule continue |
| `pending` HITL **且** 缓冲非空 | **不** schedule continue；仅 resume 步首 Apply（普通 human message 不打断 pending） |
| `POST .../clear-context` / `DELETE ...` | **丢弃** server 缓冲并发送 `side_effects_cleared`（与 Cancel 保留缓冲不同） |

实现：`session/side_effects.go`、`session/runtime_side_effects.go`、`turn/sse_publish.go` → `PublishSideEffectCallback` / `PublishSideEffectTurnStart` / `PublishSideEffectApplied` / `PublishSideEffectsCleared`。

### 2.5 Skills（可选 HTTP；也可仅 tool）

```http
GET /v1/agents/{agent_id}/skills
POST /v1/agents/{agent_id}/skills/load
POST /v1/agents/{agent_id}/skills/unload
```

与工具 `load_skills` 语义一致；HTTP 供 Client 设置页使用。

`POST /skills/load` 和 `POST /skills/unload` 的响应除 `loaded_skills` 外还包含：

```json
{
  "requested": ["writer"],
  "rejected": [],
  "changed": true,
  "session_state_applied_boundary": "immediate",
  "model_context_applied_boundary": "next_human_turn",
  "hooks_status": "synchronized",
  "hooks_loaded": [],
  "hooks_failed": []
}
```

控制面 load 是单项追加，unload 是单项移除；`load_skills` 工具仍是整组替换。`next_model_step` 只在
活动 Turn 中返回，空闲时返回 `next_human_turn`，未改变 loaded 集合时返回 `unchanged`。

### 2.6 Policy（工具 / shell；按 Agent）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/agents/{agent_id}/policy` | 返回该 Agent 的工具与 shell 策略快照 |
| PUT | `/v1/agents/{agent_id}/policy/tools` | 更新工具策略：`{"updates":[{"name":"read_file","decision":"allow_auto"}]}` |
| PUT | `/v1/agents/{agent_id}/policy/shell/{bash\|cmd\|powershell}` | 更新 shell 命令策略 |

**`decision` 枚举**：`allow_auto`（白名单 / txt `never`）· `require_approval`（需审批 / txt `always`）· `deny`（黑名单 / txt `deny`）。

写盘后 Node 热更新该 Agent runtime 的 policy engine；`ask_user_information` 禁止设为 `deny`。全局 `/v1/policy*` 已移除（404）。

Web UI：Agents 设置页 Policy 面板。

### 2.7 Context 诊断中的 Skills 成本

`GET /v1/agents/{agent_id}/context` 返回 `skills_catalog_timing`，用于诊断 Skills 目录的运行时成本：

```json
{
  "metadata_scan_count": 1,
  "metadata_scan_duration_ns": 12000,
  "body_read_count": 1,
  "body_read_duration_ns": 8000,
  "body_read_bytes": 2400,
  "body_cache_hit_count": 1,
  "boundary_digest_count": 1,
  "boundary_digest_duration_ns": 5000,
  "token_estimate_count": 1,
  "token_estimate_duration_ns": 3000
}
```

这些字段仅用于观测，不进入 system prompt、API tool schema、Catalog revision 或 provider cache key。
普通模型 Step 的 prompt 构建只加载已加载 Skill 正文，不执行全量目录 token 估算；后者属于 context
诊断路径。

---

## 2.8 临时子 Agent（Client / 用户）

契约详述见 **[child-agent-tools.md](./child-agent-tools.md)**。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/agents/{parent_agent_id}/child-agents` | 列出该父 Agent 下的活跃与最近终态子 Agent 快照 |
| GET | `/v1/agents/{parent_agent_id}/child-agents/{child_agent_id}` | 查询单个子 Agent 状态 |
| POST | `/v1/agents/{parent_agent_id}/child-agents/{child_agent_id}/cancel` | 用户/Client 停止临时 Agent（与工具 `cancel_temporary_agent` 等价） |

- 临时 Agent 由父 Agent 工具 **`create_temporary_agent`** 创建，创建工具同步等待子 Agent 终态，**无**独立 SSE；事件 **`temporary_agent_created` / `temporary_agent_progress` / `temporary_agent_completed` / `temporary_agent_cancelled`** 发往**父** Agent 的 `GET /v1/streams`。
- 子 Agent 完成、失败、取消、过期或因 Node 重启中断后，都会保留轻量 ChildRun 快照供 UI hydrate；完整 transcript 不复制到父会话。

父 Agent 工具（非 HTTP）：`create_temporary_agent`、`cancel_temporary_agent`。

---

## 3. 跨机协作（工作组）

非子 Agent 的跨机协作走 **工作组（Workgroup）**（见 [workgroup-and-node-gateway.md](../design/workgroup-and-node-gateway.md)、[handbook/05-Manage与A2A.md](../handbook/05-Manage与A2A.md)、[handbook/07-Workgroup协作.md](../handbook/07-Workgroup协作.md)）。本 Node 不提供 peer 直连派活的 HTTP 路由。

---

## 4. 子 Agent 实现分层（Go Node）

临时子 Agent **不监听端口**、**不注册 Manage 独立条目**（继承主 `agent_id`）。

| 层 | 说明 |
|----|------|
| **HTTP** | §2.8 list / get / cancel（用户与 Client） |
| **工具** | `create_temporary_agent` 等（父 Agent turn loop） |
| **进程内** | `node/internal/childagent/`：`Create` / `Cancel` / `ListSnapshots` / `RouteResume` |

字段、SSE、生命周期见 **[child-agent-tools.md](./child-agent-tools.md)**。

子 Agent turn 与工具执行共享：

- 同一 FS 根 / shell 环境
- 同一 LLM 客户端（审计日志带 `parent_agent_id`、`child_agent_kind=temporary`）

---

## 5. 工具执行（无独立 Client API）

工具由 turn loop 内部调度，**不**对 Client 暴露 `POST /tools/execute`。

执行生命周期通过 SSE 表达（**自动工具**路径中间**不发** `turn_finished`，见 §2.4.1）：

```text
# 自动工具（无 HITL）
tool_call → tool_result → assistant … → turn_finished { turn_complete: true }

# 需 HITL（审批 / ask_user）
tool_call → hitl_required { items: [ user_information?, execute_tool*, … ] }
         → turn_state { phase: tool_waiting, terminal: false }
         →（用户 resume，可多次：type=user_information / type=approval|selection）
         → tool_result → assistant … → turn_finished { turn_complete: true }
```

Node 内部分层（实现参考，非 HTTP）：

```text
TurnOrchestrator
  → PolicyEngine（本地 + Manage 下发的静态策略文件）
  → ToolRegistry（bash、fs、skills、triggers、child_agents、…）
  → Executor（os/exec、fs；工具边界见手册）
  → AuditReporter → Manage
```

---

## 6. 与 Manage 的出站调用（Node 作为客户端）

见 [manage-architecture.md](../design/manage-architecture.md) 与 [handbook/05](../handbook/05-Manage与A2A.md)：

- `POST /v1/registry/agents`、`POST /v1/registry/agents/{id}/heartbeat`
- **Registry discover**（目录）：`GET /v1/registry/agents/discover`
- **工作组**：经 Node 反代 / Dialer（见 handbook/05、07）
- **Releases**：`GET /v1/releases/check`

**无** WebSocket control channel 作为 Node↔Node 信令；**无** peer Node 直连。

---

## 7. 配置示例（与 Client 共享）

```yaml
# /etc/dagents/agent.yaml（示意）
node_id: ops-win-01
listen:
  host: 127.0.0.1
  port: 18765
manage:
  enabled: true
  url: https://manage.example.com
  registration:
    base_url: http://192.168.1.10:18765
llm:
  provider: openai
  model: gpt-4.1
```

Node 的 `runtime_root` 固定为 `./.runtime`；Agent 的 `workspace_root` 不在 Node YAML 中配置，而是在创建 Agent 时选择。

Client 同目录 `client.yaml` 仅引用：

```yaml
local:
  endpoint: http://127.0.0.1:18765
  agent_id: ops-win-01
```

---

## 8. Phase 1 最小落地集

| 优先级 | API |
|--------|-----|
| P0 | `/health`、`POST /v1/agents`、`POST /v1/messages`、`GET /v1/streams` |
| P0 | Manage 注册/心跳/审计（出站） |
| P1 | HITL resume、skills HTTP、Workgroup / Manage 工具 |
| P2 | **临时子 Agent**（[child-agent-tools.md](./child-agent-tools.md)）；execution_progress 细粒度事件仍为远期 |
