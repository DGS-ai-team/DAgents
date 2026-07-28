# Agent Node HTTP API

本文描述 **Agent Node（Go）** 对外 HTTP/SSE 接口，与 `node/internal/api/` 对齐维护。
最小 OpenAPI：[`openapi-node.yaml`](./openapi-node.yaml)。Manage API 见 [manage-architecture.md](../design/manage-architecture.md)。

## 0. 契约迁移（必读）

| 主题 | 现状 |
|------|------|
| **用户面主键** | **Agent**（`agent_id`）。1 Agent = 1 主对话；Session 仅为内部实现 |
| **推荐路径** | `/v1/agents/{agent_id}/...`（ensure / hydrate / context / cancel / skills / child-agents / media / **policy** / **prompt-context**） |
| **`/v1/sessions*`** | **已下线（410）**；错误码 `sessions_moved`；请改用 `/v1/agents/{agent_id}/...` |
| **`/v1/policy*`** | **已下线（410）**；策略按 Agent 存 `agents.db`（`agent_policy` 表） |
| **侧车 Markdown** | 按 Agent 存 `agents.db`（`agent_prompt_context`）；开关仍在 `config_snapshot_json.defaults.prompt_context` |
| **Manage 注册** | 载荷主字段为 `node_id`（`agent_id` 仅为兼容旧 Manage，值同 `node_id`）；`manage.enabled` 默认关 |
| **LLM active** | 不再因切换 Agent 抢占进程级 active；绑定写在该 Agent 快照 `defaults.llm.active` |

## 1. 设计原则

| 原则 | 说明 |
|------|------|
| **一进程多 Agent** | Node 进程持有多个 Agent 实例；对外按 `agent_id` 寻址 |
| **Client 仅本地** | 默认只 bind `127.0.0.1`；Client 与 Node 同包读同一 `local.endpoint` |
| **思考与工具在 Node 内** | 无「Backend 代执行」路径；tool call 由 turn loop 本地完成 |
| **A2A 经 Manage** | 非子 Agent 禁止 peer 入站；`expose_to_peers` 仅控制是否可作为 A2A **目标** |
| **会话态在 Node** | Agent 对话上下文、队列、持久化由 Node 负责（SQLite） |

### 1.1 基础路径

| 前缀 | 调用方 | 说明 |
|------|--------|------|
| `/v1/agents/...` | Client（本地） | **主契约**：对话、策略、侧车、子 Agent |
| `/v1/...` | Client（本地） | messages、streams、triggers、setup、llm |
| `/v1/sessions/...` | 已下线（410） | 见 §0 |
| `/health` | 探活 | 负载均衡 / 运维脚本 |

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

常见 `code`：`invalid_agent`、`agent_not_found`、`turn_busy`、`policy_denied`、`approval_required`、`a2a_target_not_exposed`、`llm_error`、`tool_error`、`policy_moved`、`sessions_moved`。

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

`version` 字段为全项目唯一语义化版本（源码：`node/internal/version/version.go`）。历史字段名 `agent_id` 此处表示 **Node 身份**（同 `node_id`）。

```http
GET /v1/agent/info
```

```json
{
  "agent_id": "ops-win-01",
  "expose_to_peers": true,
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
  "defaults": { "llm": { "active": "deepseek", "max_tool_loops": 32 } },
  "sandbox": { "enabled": false, "backend": "process" }
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

全局 `GET/PUT /v1/policy*` 返回 **410 Gone**。

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

### 2.5 Sessions（已下线）

`/v1/sessions*` 固定返回 **410 Gone**（`sessions_moved`），对齐已下线的 `/v1/policy*`。对话、hydrate、context、cancel、skills、child-agents、media 一律走 `/v1/agents/{agent_id}/...`。

### 2.6 消息与 resume

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
GET /v1/streams?agent_id=agt-xxx
Accept: text/event-stream
Last-Event-ID: 42
```

- Phase 1 可简化为 **全局单流**（一个 Client 一个 Node 实例通常一个活跃 session）。
- 帧格式见 [附录/SSE事件速查.md](../handbook/附录/SSE事件速查.md)。

核心事件：`assistant`、`reasoning`、`tool_call`、`tool_result`、`hitl_required`、`user_message_deferred`、`side_effect_turn_start`、`side_effect_applied`、`side_effects_cleared`、`temporary_agent_created` / `temporary_agent_completed` / `temporary_agent_cancelled`、`error`、`done`。

**兼容 / 中继**：A2A caller 中继与子 Agent 审批仍可能发送 `approval_required` / `user_information_required`（见 §3、 [manage-communication.md](../manage-communication.md)）。**本地 turn** 统一为 `hitl_required`。

#### 2.4.1 `done` 事件（语义 B：轮到用户）

`done` **仅**表示编排器在当前步暂停、**轮到用户交互**（解锁 Client 等待、进入 HITL 或自由输入）。**不**表示 assistant 流式段落结束（换行由 `assistant` / `tool_call` / `reasoning` 等事件在 Client 侧收束）。

实现：`node/internal/turn/sse_publish.go` 中 `publishDone`。

**载荷字段**

| 字段 | 类型 | 说明 |
|------|------|------|
| `finish_reason` | string | `stop` \| `awaiting_hitl` \| `error` \| `cancelled`（历史兼容：`awaiting_user_information` / `awaiting_tool_approval` 仍映射为 HITL 暂停） |
| `turn_complete` | bool | `true`：本条用户 `message` 驱动的链已结束，可自由输入；`false`：HITL 暂停，链未结束 |
| `awaiting` | string \| null | HITL 暂停时：`hitl`；链结束时为 `null`（历史：`user_information` / `tool_approval`） |

**何时发送 `done`**

| 场景 | 发送 | `turn_complete` | `awaiting` |
|------|------|-----------------|------------|
| 模型一步结束且无 `tool_calls` | ✅ | `true` | `null` |
| `ask_user_information` / 审批 pending | ✅ | `false` | `hitl` |
| LLM/工具错误、取消、超循环上限 | ✅ | `true` | `null` |
| 自动工具执行后 `tool_result` 续跑 | ❌ | — | — |
| 客户端 `resume` 刚处理完、链继续 | ❌ | — | — |

**一次 `submit` 可有多条 `done`**（例如连续多轮 `ask_user_information`），每次 HITL 暂停一条；最终以 `finish_reason=stop` 且 `turn_complete=true` 收束。

**与其它事件分工**

- **`hitl_required`**：本地 turn 统一 HITL 事件；`items[]` 每项含 `hitl_type`：`user_information`（`ask_user_information`）或 `execute_tool`（需审批工具）。Client 按 item 类型展示并分别 `POST resume`；同批可混合 ask + approval，Node 侧为单一 `PendingHITL.Items`。
- **`approval_required` / `user_information_required`**：A2A caller 中继、子 Agent 审批等路径仍使用；Client 兼容处理。
- `tool_call`（含 `ask_user_information`）：工具行展示；**不**替代 HITL 块。
- 子 Agent 内部 `done`：**不**转发到父 SSE（`node/internal/childagent/relay_hub.go`）。

**Client**（Go / Python Textual / Web UI）：收到 `hitl_required` 后展开为 HITL 队列（先 user_information item，再 execute_tool 合并为 approval 面板）；每步 resume 后 Node 可部分消 pending，全部 resolved 才续跑 tool loop。`submit_message` 后 `wait_user_turn` 等待语义 B 的 `done`（`turn_complete=false` 时 HITL 暂停正常结束等待）。

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

async / trigger / a2a inbox 在 **任务未完成**（HITL、open batch、tool loop）时 **Produce**：立即 SSE、写入 `sideEffectStore`，**不改** `runtime.messages`、**不**跑 LLM。Apply 在 `runTurnStep` 步首；被动续跑经内部队列 `side_effect_continue`（与 `tool_result` 同优先级 -1）。

| 事件 | 何时发送 | 典型 `data` | Client 行为 |
|------|----------|-------------|-------------|
| `user_message_deferred` | external Produce 且 tail 为**任务已完成桥接态**（纯 assistant stop） | `content`、`user_name`、`deferred: true`、`side_effect_seq?`、`trigger_id?` | transcript 插入 **deferred** 样式 user 行；**不** `finishTurn` |
| `side_effect_turn_start` | `handleSideEffectContinue` **跑 LLM 之前** | `source`（`side_effect_continue` \| `cancel_recovery` \| `task_complete_produce`）、`side_effect_pending`、`implicit_turn: true` | **`beginImplicitTurn` / `beginSubmit`**，进入被动等待态 |
| `side_effect_applied` | `ApplyReady` 成功写入 history 后 | `seqs: number[]` | 将 `side_effect_seq` 匹配的 deferred / callback 行标为 **已入库** |
| `side_effects_cleared` | `ClearContext` / `Delete` 丢弃 server 缓冲 | `dropped`、`seqs: number[]` | 将未入库条目标为 **已失效** |
| `tool_call` / `tool_result`（Produce） | async / 非桥接 external Produce | 与正常工具事件相同，可含 `deferred: true`、`side_effect_seq` | idle 时仍渲染；**不**因 Produce 单独 `finishTurn` |

**Produce vs Apply**

| 时刻 | Server history | Client transcript |
|------|----------------|-------------------|
| Produce ×N | 缓冲 | N 条 callback / deferred user（SSE 已发） |
| Apply（步首，可合并 `get_callback`） | 写入 1 条或多条 | **`side_effect_applied`**（无新 callback SSE） |
| Continue LLM | 正常 assistant 流 | 流式 + `done` |

**`implicit_turn` 语义**：**非**用户 `POST /v1/messages` 驱动的 turn。Client 收到 `side_effect_turn_start` 后应开启与 user submit 相同的 turn 栅栏（Go `TurnGate.BeginImplicitTurn`、Web `beginImplicitTurn`、Python `begin_implicit_turn`），以便接收后续 `assistant` / `done`。

**Cancel 与缓冲**（`POST /v1/agents/{id}/cancel`）：

| 条件 | 行为 |
|------|------|
| 在途 LLM（`state != idle`） | `turnCtx` cancel → `done(cancelled)` |
| 缓冲非空 **且** `pending == nil` | 额外 `scheduleSideEffectContinue("cancel_recovery")` → `side_effect_turn_start` → Apply + LLM |
| 缓冲空 | 仅 abort，**不** schedule continue |
| `pending` HITL **且** 缓冲非空 | **不** schedule continue；resume / human 步首 Apply |
| `POST .../clear-context` / `DELETE ...` | **丢弃** server 缓冲并发送 `side_effects_cleared`（与 Cancel 保留缓冲不同） |

实现：`session/side_effects.go`、`session/runtime_side_effects.go`、`turn/sse_publish.go` → `PublishExternalSideEffectDeferred` / `PublishSideEffectTurnStart` / `PublishSideEffectApplied` / `PublishSideEffectsCleared`。

### 2.5 Skills（可选 HTTP；也可仅 tool）

```http
GET /v1/agents/{agent_id}/skills
POST /v1/agents/{agent_id}/skills/load
POST /v1/agents/{agent_id}/skills/unload
```

与工具 `load_skills` 语义一致；HTTP 供 Client 设置页使用。

### 2.6 Policy（工具 / shell 黑白名单）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/policy` | 返回工具与 shell 策略快照；可选 `?shell=bash\|cmd\|powershell\|auto` |
| PUT | `/v1/policy/tools` | 更新工具策略：`{"updates":[{"name":"read_file","decision":"allow_auto"}]}` |
| PUT | `/v1/policy/shell/{bash\|cmd\|powershell}` | 更新 shell 命令策略：`{"updates":[{"command":"rm","decision":"deny"}]}` |

**`decision` 枚举**：`allow_auto`（白名单 / txt `never`）· `require_approval`（需审批 / txt `always`）· `deny`（黑名单 / txt `deny`）。

**`platform`**：`goos` 为 Node 进程 OS；`default_shell` 与 `bash_run` 默认 shell 一致（Windows→`powershell`，其余→`bash`）。

写盘后 Node 热更新全部活跃 session 的 policy engine；`ask_user_information` 禁止设为 `deny`。

Client 入口：`/policy` 全屏界面（Go bubbletea / Python Textual，Esc 返回）。

---

## 2.8 临时子 Agent（Client / 用户）

契约详述见 **[child-agent-tools.md](./child-agent-tools.md)**。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/agents/{parent_agent_id}/child-agents` | 列出该父 Agent 下**未交付**的活跃子 Agent |
| GET | `/v1/agents/{parent_agent_id}/child-agents/{child_agent_id}` | 查询单个子 Agent 状态 |
| POST | `/v1/agents/{parent_agent_id}/child-agents/{child_agent_id}/cancel` | 用户/Client 停止临时 Agent（与工具 `cancel_temporary_agent` 等价） |

- 临时 Agent 由父 Agent 工具 **`create_temporary_agent`** 创建（非 A2A），**无**独立 SSE；事件 **`temporary_agent_created` / `temporary_agent_completed` / `temporary_agent_cancelled`** 发往**父** session 的 `GET /v1/streams`。
- 子 Agent **生命周期**在**向父 Agent 交付结果**后结束并回收；交付时发送结束类 SSE。

父 Agent 工具（非 HTTP，非 A2A）：`create_temporary_agent`、`wait_temporary_agents`、`temporary_agent_status`、`cancel_temporary_agent`。

---

## 3. A2A（经 Manage，无入站 API）

非子 Agent 的 A2A **不**在本 Node 暴露 HTTP 路由；由工具层调用 **Manage**（见 [a2a-via-manage.md](../future/a2a-via-manage.md)、[manage-architecture.md](../design/manage-architecture.md) §3.2）。

| Node 内工具 | Manage API |
|-------------|------------|
| `agent_discover` | `GET /v1/registry/agents/discover` |
| `agent_invoke` | `POST /v1/a2a/tasks` + 轮询 `GET /v1/a2a/tasks/{id}` |
| `agent_notify` | `POST /v1/a2a/tasks`（`kind=notify`） |

**Inbox long poll**（`node/internal/manage/inbox_poller.go`）：

```http
GET {manage_url}/v1/a2a/inbox?agent_id={self}&limit=10&wait=25
```

拉取到的 Task 入本地 **A2A session** 队列，走 turn loop；处理完成后：

```http
POST {manage_url}/v1/a2a/tasks/{task_id}/reply
```

---

## 4. 子 Agent 实现分层（Go Node）

临时子 Agent **不监听端口**、**不注册 Manage 独立条目**（继承主 `agent_id`）。

| 层 | 说明 |
|----|------|
| **HTTP** | §2.8 list / get / cancel（用户与 Client） |
| **工具** | `create_temporary_agent` 等（父 Agent turn loop） |
| **进程内** | `node/internal/childagent/`：`Create` / `Deliver` / `Cancel` / `Wait` |

字段、SSE、生命周期见 **[child-agent-tools.md](./child-agent-tools.md)**。

子 Agent turn 与工具执行共享：

- 同一 FS 根 / shell 环境
- 同一 LLM 客户端（审计日志带 `parent_agent_id`、`child_agent_kind=temporary`）

---

## 5. 工具执行（无独立 Client API）

工具由 turn loop 内部调度，**不**对 Client 暴露 `POST /tools/execute`。

执行生命周期通过 SSE 表达（**自动工具**路径中间**不发** `done`，见 §2.4.1）：

```text
# 自动工具（无 HITL）
tool_call → tool_result → assistant … → done { turn_complete: true }

# 需 HITL（审批 / ask_user）
tool_call → hitl_required { items: [ user_information?, execute_tool*, … ] }
         → done { turn_complete: false, awaiting: hitl, finish_reason: awaiting_hitl }
         →（用户 resume，可多次：type=user_information / type=approval|selection）
         → tool_result → assistant … → done { turn_complete: true }
```

Node 内部分层（实现参考，非 HTTP）：

```text
TurnOrchestrator
  → PolicyEngine（本地 + Manage 下发的静态策略文件）
  → ToolRegistry（bash、fs、skills、triggers、agent_discover、agent_invoke、…）
  → Executor（os/exec、fs、sandbox）
  → AuditReporter → Manage
```

---

## 6. 与 Manage 的出站调用（Node 作为客户端）

见 [manage-architecture.md](../design/manage-architecture.md) 与 [manage-communication.md](../manage-communication.md)：

- `POST /v1/registry/agents`、`POST /v1/registry/agents/{id}/heartbeat`
- **A2A**：`GET /v1/registry/agents/discover`、`POST /v1/a2a/tasks`、`GET /v1/a2a/inbox`、`POST .../tasks/{id}/reply`、`GET /v1/a2a/tasks/{id}`
- **Releases**：`GET /v1/releases/check`

**无** WebSocket control channel；**无** peer Node 直连。

---

## 7. 配置示例（与 Client 共享）

```yaml
# /etc/dagents/agent.yaml（示意）
agent_id: ops-win-01
listen:
  host: 127.0.0.1
  port: 18765
expose_to_peers: true
groups: [ops, windows]
manage:
  url: https://manage.example.com
  node_token: "${MANAGE_NODE_TOKEN}"
fs_root: D:\agent-workspace
llm:
  provider: openai
  model: gpt-4.1
```

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
| P1 | HITL resume、skills HTTP、A2A inbox 轮询 + Manage 工具 |
| P2 | **临时子 Agent**（[child-agent-tools.md](./child-agent-tools.md)）— **已实现**；execution_progress 细粒度事件仍为远期 |
