# 临时 Agent（temporary agent）：工具、HTTP API 与 SSE（Go Node 定稿）

本文是 **Go Agent Node** 同进程**临时 Agent** 的**实现契约**（Phase 1）。与外部 **A2A** 对等调用无关。概念背景见 [temporary-child-agents.md](../future/temporary-child-agents.md)；HTTP 总览见 [agent-node-api.md](./agent-node-api.md) §2.8。

**状态**：Go Node **已实现**（Phase 1）；Client TUI 适配待后续 PR。

---

## 1. 设计原则

| 原则 | 说明 |
|------|------|
| **同进程、同 `agent_id`** | 子 Agent 不监听端口、不注册 RC/Manage |
| **父 session 归属** | 仅创建它的父 session 可 wait/status/cancel |
| **生命周期** | 自 `create` 至**向父 Agent 交付结果**为止；交付后立即回收子 runtime |
| **SSE 挂在父 session** | 创建与结束事件均发往**父** `session_id` 的 SSE 流 |
| **权限子集** | `allowed_tools` ⊆ 父可用工具；子 Agent 不可再创建子 Agent |
| **审批** | `create_temporary_agent` 在 `rule` 审批模式下**默认需审批**（与 `bash_run` 同级） |

---

## 2. 工具清单

| 工具名 | 调用方 | 说明 |
|--------|--------|------|
| `create_temporary_agent` | 父 Agent | 创建并启动临时 Agent（非 A2A） |
| `wait_temporary_agents` | 父 Agent | 等待多个临时 Agent 终态并汇总 |
| `temporary_agent_status` | 父 Agent | 非阻塞查询状态 |
| `cancel_temporary_agent` | 父 Agent | 显式取消仍在运行的临时 Agent |

临时 Agent **不得**注册或调用以上工具，以及 `trigger_*`、`ask_user_information`、A2A 工具。

---

## 3. `create_temporary_agent`

### 3.1 请求字段

| 字段 | 类型 | 必填 | 默认 | 约束 |
|------|------|------|------|------|
| `task` | string | **是** | — | 子 Agent 首条 user 消息；须自包含（含角色与约束），禁止依赖向用户追问 |
| `purpose` | string | **是** | — | 短说明（日志 / SSE / 审计） |
| `allowed_tools` | string[] | 否 | 见 §7 | 须 ⊆ 父可下放工具（§8） |
| `ttl_seconds` | integer | 否 | 1800 | `60`～`child_agents.max_ttl_seconds` |
| `max_turns` | integer | 否 | 20 | `1`～`child_agents.max_max_turns` |
| `wait` | boolean | 否 | `false` | `true`：阻塞至子 Agent 交付结果后返回 |

**不支持** `run_in_background`（由 orchestrator 专用处理）。

### 3.2 返回（JSON 字符串）

**`wait=false` → Handle：**

```json
{
  "kind": "handle",
  "child_session_id": "child-a1b2c3d4e5f6",
  "status": "active",
  "purpose": "review patch",
  "expires_at": "2026-05-30T12:30:00+08:00",
  "max_turns": 20
}
```

**`wait=true` → Result（终态，已交付）：**

```json
{
  "kind": "result",
  "child_session_id": "child-a1b2c3d4e5f6",
  "status": "completed",
  "summary": "……",
  "turn_count": 5,
  "artifacts": []
}
```

`status`：`completed` | `failed` | `cancelled` | `expired`

失败（参数非法、超并发等）：`ERROR: ...`，且不创建子 session。

---

## 4. `wait_temporary_agents`

| 字段 | 类型 | 必填 | 默认 |
|------|------|------|------|
| `child_session_ids` | string[] | **是** | — |
| `timeout_seconds` | integer | 否 | `300` |
| `fail_fast` | boolean | 否 | `false` |

- `timeout_seconds=0`：立即返回当前快照（不阻塞）。
- `fail_fast=true`：任一子 Agent 进入 `failed/cancelled/expired` 且其余未结束时提前返回。

```json
{
  "timed_out": false,
  "results": [
    {
      "child_session_id": "child-a1b2c3d4e5f6",
      "status": "completed",
      "summary": "……",
      "turn_count": 5,
      "error": ""
    }
  ]
}
```

---

## 5. `temporary_agent_status`

| 字段 | 类型 | 必填 |
|------|------|------|
| `child_session_ids` | string[] | **是**（≥1） |

返回 `wait_temporary_agents.results` 同结构的数组（无 `timed_out`）。

---

## 6. `cancel_temporary_agent`

| 字段 | 类型 | 必填 |
|------|------|------|
| `child_session_id` | string | **是** |
| `reason` | string | 否 |

返回：

```json
{
  "child_session_id": "child-a1b2c3d4e5f6",
  "status": "cancelled",
  "previous_status": "active"
}
```

已终态则幂等返回当前 `status`。

---

## 7. 默认工具与 task 约定

**无预设模板**。父 Agent 须在 `task` 中写明子任务角色、约束与交付物；Node 仅附加简短系统前缀后作为子 Agent 首条 user 消息。

| 项 | 默认 |
|----|------|
| `allowed_tools`（未指定时） | `read_file`, `search_file`, `bash_run` |
| `max_turns`（未指定时） | `child_agents.default_max_turns`（默认 20） |

`allowed_tools` 须为 §8 可下放列表的子集，并与父 Agent 全局可用工具求交。临时 Agent **不得**使用 `load_skills` / `unload_skills` / `clear_skills`（仅父 Agent 会话可加载技能）。

---

## 8. 工具权限

**可下放（父有则子可申请）：**

`read_file`, `write_file`, `search_file`, `search_replace`, `bash_run`, `background_job_status`, `background_job_cancel`

**永不下放：**

`create_temporary_agent`, `wait_temporary_agents`, `temporary_agent_status`, `cancel_temporary_agent`, `load_skills`, `unload_skills`, `clear_skills`, `ask_user_information`, `trigger_*`

---

## 9. 生命周期与 SSE

### 9.1 状态机

```text
creating → active → completed | failed | cancelled | expired
                              ↓
                    向父 Agent 交付结果
                              ↓
                    SSE temporary_agent_completed
                              ↓
                    回收子 runtime（生命周期结束）
```

**核心规则**：子 Agent **仅在向父 Agent 交付结果后**结束生命周期（停止 consumer、从内存表移除）。不存在「交付后继续存活」。

交付方式：

| 模式 | 交付时机 |
|------|----------|
| `wait=true` | 工具返回 JSON `kind=result` 前交付 |
| `wait=false` | 临时 Agent 终态时写入结果缓存；父调用 `wait_temporary_agents` / `temporary_agent_status` 读取即视为已交付；**首次被父读取终态结果时**发 `temporary_agent_completed` 并回收（若尚未回收） |

> **实现备注**：为简化 Client，推荐在子 Agent **达到终态瞬间**即发 `temporary_agent_completed` 并交付内部结果缓存；父 Agent 通过 wait/status 或 sync tool 读取同一结果对象。回收在 SSE 发送之后同步进行。

### 9.2 SSE 事件（发往 **父** `session_id`）

与现有 Hub 一致：`hub.Publish(parentSessionID, agentID, eventType, data)`。

#### `temporary_agent_created`

创建成功、子 Agent 即将/已经开始消费 `task` 时发送（`wait`  true/false 均发送）。

```json
{
  "child_session_id": "child-a1b2c3d4e5f6",
  "parent_session_id": "sess-7f2a...",
  "purpose": "review patch",
  "status": "active",
  "expires_at": "2026-05-30T12:30:00+08:00",
  "max_turns": 20,
  "wait": false
}
```

#### `temporary_agent_completed`

子 Agent 向父 Agent **交付结果**、生命周期结束前发送（`status=completed|failed`）。

```json
{
  "child_session_id": "child-a1b2c3d4e5f6",
  "parent_session_id": "sess-7f2a...",
  "status": "completed",
  "summary": "……",
  "turn_count": 5,
  "artifacts": []
}
```

#### `temporary_agent_cancelled`

因 `cancel_temporary_agent`、HTTP cancel、TTL、父 session 清理而取消，交付 `cancelled` 结果后发送。

```json
{
  "child_session_id": "child-a1b2c3d4e5f6",
  "parent_session_id": "sess-7f2a...",
  "status": "cancelled",
  "reason": "user requested stop",
  "previous_status": "active"
}
```

`expired` 可复用 `temporary_agent_completed`，`status=expired`，或单独 `temporary_agent_expired`（实现二选一，推荐统一进 `temporary_agent_completed`）。

### 9.3 Client 适配（后续 PR）

Go/Python Client 在父 session SSE 上识别上述事件，在 TUI 展示「子任务已创建 / 已完成 / 已取消」。Phase 1 可先打 `[system]` 行。

---

## 10. HTTP API（用户 / Client 停止子 Agent）

子 Agent **无**独立 SSE 连接；用户通过**父 session** 订阅流，并通过以下 API 管理。

前缀：`/v1/sessions/{parent_session_id}/child-agents`

`parent_session_id` 须为普通用户 session（非 `child-*`）。

### 10.1 列出活跃子 Agent

```http
GET /v1/sessions/{parent_session_id}/child-agents
```

响应：

```json
{
  "parent_session_id": "sess-7f2a...",
  "items": [
    {
      "child_session_id": "child-a1b2c3d4e5f6",
      "status": "active",
      "purpose": "review patch",
      "allowed_tools": ["read_file", "search_file"],
      "created_at": "2026-05-30T12:00:00+08:00",
      "expires_at": "2026-05-30T12:30:00+08:00",
      "turn_count": 2,
      "max_turns": 20
    }
  ]
}
```

仅返回 **尚未交付结果**（`creating` / `active`）的条目。

### 10.2 查询单个子 Agent

```http
GET /v1/sessions/{parent_session_id}/child-agents/{child_session_id}
```

- 活跃：返回与 list item 相同字段。
- 已交付并回收：`404`，`code=child_agent_not_found`。

### 10.3 停止（取消）子 Agent

```http
POST /v1/sessions/{parent_session_id}/child-agents/{child_session_id}/cancel
Content-Type: application/json

{ "reason": "user requested stop" }
```

`reason` 可选。

响应（200）：

```json
{
  "child_session_id": "child-a1b2c3d4e5f6",
  "status": "cancelled",
  "previous_status": "active"
}
```

行为与工具 `cancel_temporary_agent` **相同**：

1. 取消在途 turn；
2. 构造 `cancelled` 结果并交付父 Agent（写入 wait 缓存）；
3. SSE `temporary_agent_cancelled`；
4. 回收子 runtime。

错误：

| code | 场景 |
|------|------|
| `session_not_found` | 父 session 不存在 |
| `child_agent_not_found` | id 不存在或不属于该父 session |
| `child_agent_already_finished` | 已交付（幂等 200 返回终态亦可） |

---

## 11. 配置

```yaml
child_agents:
  enabled: true
  default_ttl_seconds: 1800
  max_ttl_seconds: 7200
  default_max_turns: 20
  max_max_turns: 50
  max_active_per_parent: 8
  default_wait_timeout_seconds: 300
```

---

## 12. 标识符

- `child_session_id`：`child-` + 12 位 hex（例 `child-a1b2c3d4e5f6`）
- 不出现在 `GET /v1/sessions` 默认列表

---

## 13. 审批策略（policy）

在 `.runtime/policy/` 规则模式下：

- `create_temporary_agent`：**需要审批**（建议默认规则与 `bash_run` 同级或单独 `risk=high`）
- `cancel_temporary_agent`：不需要审批（用户/Client 可通过 HTTP 直接 cancel）
- `wait_temporary_agents` / `temporary_agent_status`：只读，不审批

临时 Agent **内部**工具调用的审批见 §14（与「创建临时 Agent」的审批是两层不同 HITL）。

---

## 14. 子 Agent 工具审批（HITL）

### 14.1 原则

| 原则 | 说明 |
|------|------|
| **策略不弱于父** | 子 Agent 走**同一** `policy.Engine`；父需审批的工具，子调用时**同样**需审批 |
| **不向子 session 发 SSE** | Client 只订阅**父** `session_id`；子 Agent 的 HITL 事件一律挂在父 SSE 流上 |
| **不向用户追问** | 子 Agent **无** `ask_user_information`；任务须在 `task` 中写全，审批仅针对**工具执行** |
| **resume 走父 session** | `POST /v1/messages` 的 `session_id` 始终是**父 session**；Node 按 `child_session_id` 路由到子 runtime |

### 14.2 与「创建子 Agent」审批的区别

```text
父 turn：create_temporary_agent  →  approval_required（scope=parent，无 child_session_id）
                                      ↓ 用户批准
                               创建 temporary agent，SSE temporary_agent_created

子 turn：bash_run 等            →  approval_required（scope=child，带 child_session_id）
                                      ↓ 用户批准
                               子 runtime ContinueAfterResume，继续子 turn
```

两层 HITL **互不覆盖**：各自 `PendingHITL` 存在对应 runtime（父 / 子）内存中。

### 14.3 SSE：`approval_required` 扩展字段

子 Agent 触发审批时，仍用事件类型 **`approval_required`**（Client 现有 HITL 逻辑可复用），在 `data` 中增加：

```json
{
  "approval_type": "execute_tool",
  "approval_id": "appr-xxx",
  "execution_id": "exec-xxx",
  "message": "临时 Agent 请求执行工具，等待确认。",
  "approval_args": {
    "tool_calls": [
      {
        "id": "call-1",
        "name": "bash_run",
        "arguments": { "command": "..." }
      }
    ]
  },
  "display_type": "normal_text",
  "child_session_id": "child-a1b2c3d4e5f6",
  "child_purpose": "review patch",
  "hitl_scope": "temporary_agent"
}
```

| 字段 | 说明 |
|------|------|
| `hitl_scope` | `"temporary_agent"` 表示临时 Agent 工具审批；省略或 `"parent"` 表示父 Agent 自身 turn |
| `child_session_id` | 子 session，resume 路由用 |
| `child_purpose` | 创建时的 purpose，供 TUI 展示上下文 |

**可选**辅助事件 `child_agent_awaiting_approval`（仅 UI 提示「某子任务等待审批」）；**不替代** `approval_required` 作为 resume 触发源。

### 14.4 Resume 路由

请求（**父** session_id）：

```http
POST /v1/messages
Content-Type: application/json

{
  "session_id": "sess-parent-7f2a",
  "request_type": "resume",
  "resume_value": {
    "type": "approve",
    "child_session_id": "child-a1b2c3d4e5f6",
    "approval_id": "appr-xxx"
  }
}
```

`selection` / `reject` 与父 Agent 相同，**必须**带 `child_session_id`（当 `hitl_scope=temporary_agent` 时）。

Node 逻辑：

1. `EnqueueMessage(parent_session_id, resume)`；
2. 若 `resume_value.child_session_id` 非空 → `RouteResume` 投递到**子 runtime** 的 `handleResume`；
3. 否则 → `RouteResume` 经 `DeliverParentResume` 入队父 runtime **一次**（勿重复 enqueue）。

父 session HITL 的 `done` 带 `turn_complete` / `awaiting`，语义见 [agent-node-api.md §2.4.1](./agent-node-api.md)。

Client（Go full / Python Textual）在收到带 `child_session_id` 的 `approval_required` 时，展示时标注「子任务：{purpose}」，**SubmitResume 仍用父 session_id**，并在 `resume_value` 中回传 `child_session_id`。

### 14.5 用户拒绝 / 超时

| 用户操作 | 子 Agent 行为 | 对父 Agent 的影响 |
|----------|---------------|-------------------|
| **reject**（拒绝工具） | 对应 tool 写入拒绝结果，子 turn **继续**（模型可改策略或结束） | 无直接中断；子最终 `summary` 可含「因审批未通过未能执行」 |
| **reject all / 连续拒绝导致无法完成** | 子 turn 以 `failed` 或 `completed`（带失败说明）终态 | 交付结果时 `status` 反映实际情况 |
| **长期不审批** | 子 Agent 保持 `active`，占用 `max_active_per_parent` 槽位；**TTL 仍计时** | TTL 到期 → `expired`，SSE `temporary_agent_completed(status=expired)` |
| **HTTP cancel 子 Agent** | 立即 `cancelled`，pending HITL 作废 | 同 §10.3 |

子 Agent **不得**因审批 pending 而向父 session 发送 `user_information_required`。

### 14.6 并发场景

**场景 A — `wait=true` 串行**

父 turn 阻塞在 `create_temporary_agent` 工具内；子 Agent 审批事件出现在父 SSE 上，用户批准后**仅**子 runtime 续跑，父 turn 仍等待子交付结果。

**场景 B — `wait=false` 并行**

父 Agent 可继续对话；同时一个或多个子 Agent 各自 pending 审批：

- 每个 `approval_required` 带不同 `child_session_id`；
- 用户按事件顺序或 TUI 列表逐条审批；
- 同一子 Agent 一批 tool call 仍合并为**一条** `approval_required`（与父 turn 多工具审批一致）。

**场景 C — 父与子同时 pending（少见）**

例如父 turn 自身 `bash_run` 待批，且异步子 Agent 也待批：

- 两条 `approval_required`，以 `hitl_scope` + `child_session_id` 区分；
- resume **必须**带正确 `child_session_id` 或省略（父）；
- Node **禁止**把 resume 误投到错误 runtime（误投返回 400 `hitl_target_mismatch`）。

### 14.7 实现要点（Go Node）

- 子 `runtime` 复用 `turn.Orchestrator`；`hub.Publish` 包装为「子 session 事件 → 父 session_id + 子元数据」。
- `PendingHITL` 持久化在子 session 的 `runtime_state_json`（若启用 SQLite）；恢复时仍从父 SSE 收 approval。
- 审计日志字段：`parent_session_id`、`child_session_id`、`hitl_scope=temporary_agent`、`approval_id`。

---

## 15. 调用范例

**串行：**

```json
create_temporary_agent({
  "purpose": "审查 login 改动",
  "task": "你是代码审查助手，只读检查 src/auth/login.go，列出风险，不要改文件",
  "allowed_tools": ["read_file", "search_file"],
  "wait": true
})
```

**并行：**

```json
create_temporary_agent({"purpose":"查日志","task":"……","wait":false})
wait_temporary_agents({"child_session_ids":["child-aaa","child-bbb"],"timeout_seconds":600})
```

**用户 HTTP 停止：**

```http
POST /v1/sessions/sess-7f2a/child-agents/child-aaa/cancel
{"reason":"用户点击停止"}
```

---

## 16. 实现分期

| PR | 内容 |
|----|------|
| 1 | `childagent` 包 + 工具 schema + SSE + 单测 |
| 2 | `create_temporary_agent` / `wait` 串行 + HTTP list/get/cancel |
| 3 | 并行 wait + Client SSE / **子 Agent HITL**（§14）展示 |
| 4 | policy 审批 + 配置 + 文档同步 |

---

## 17. 与 v2 设计 doc 差异

| v2 doc | Go Node Phase 1 |
|--------|-----------------|
| `context_seed` | 使用 **`task`** |
| `body_binding` | 省略（等价 backend_only） |
| 独立 agent_id | 共用 Node `agent_id` |
| Internal only API | 增加 §10 HTTP cancel/list（本地 Client 用） |
