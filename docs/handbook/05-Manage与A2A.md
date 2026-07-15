# 05 · Manage 与 A2A

## 本章回答什么问题

读完本章，你应能：

- 说明 Manage 与 Node、Client 的连接关系  
- 跟读 Node 注册、inbox 轮询、callee turn 执行  
- 理解 A2A Task 状态机与 HITL 中继（caller/callee 双端）  
- 使用 `cases/a2a-manage-docker` 做端到端验证  

---

## 1. Manage 角色

```text
┌─────────────┐         ┌────────────────────────────┐         ┌─────────────┐
│   Client    │  本地   │      Manage (:8020)         │  出站   │ Agent Node  │
│             │────────►│ Registry · A2A · Console    │◄────────│   (Go)      │
└──────▲──────┘         └────────────────────────────┘         └──────▲──────┘
       └────────────────── Client 只连 Node ──────────────────────────┘
```

| 参与方 | 连 Manage？ | 说明 |
|--------|-------------|------|
| Client | **否** | 会话全在 Node |
| Agent Node | **是**（出站） | 注册、心跳、A2A、审计 |
| Console | **是** | 浏览器运维 |
| Node ↔ Node | **经 Manage** | **禁止直连** |

Manage **不**跑 turn loop、**不**代理 Client 消息；A2A 信令与 inbox **由 Manage 持久化**。

**源码**：`manage/manage_app.py`、`manage/registry/`、`manage/a2a/`

---

## 2. 注册与 Agent Card

### 2.1 启动流程

```text
manage.enabled=true
  → api.NewServer: NewRegistrar + Start(ctx)
  → POST /v1/registry/agents（agent_id, base_url, capabilities, card, expose_to_peers…）
  → 周期 POST .../heartbeat
  → 可选 NewInboxPoller（a2a.enabled）
```

**Node 源码**：

| 组件 | 文件 |
|------|------|
| Registrar | `node/internal/manage/registrar.go` |
| Agent Card | `node/internal/manage/a2a_profile.go`（`RegistrationCard`） |
| Inbox 轮询 | `node/internal/manage/inbox_poller.go` |

### 2.2 Agent Card

- 工作目录固定文件名 **`agent-card.json`**（不可经 config 改路径）  
- 示例：`packaging/agent-client/agent-card.example.json`（被调方）、`agent-card.example.ops.json`（纯调用方）  
- `metadata.role` 等影响 **callee 本地处理策略**（如 compliance）；与 `expose_to_peers` 独立  

### 2.3 发现

- Node 工具 `agent_discover` → `GET /v1/registry/agents/discover`  
- 返回可协作 `agent_id` 列表；**不含** peer 直连 URL（运维 `GET .../agents/{id}` 可有 `base_url`）  
- `expose_to_peers: false` 的 Agent **不可**作为 A2A 被调目标  

---

## 3. A2A Task 模型

### 3.1 基本流（无 HITL）

```text
Caller: agent_invoke → POST /v1/a2a/tasks
Manage: 写入 Callee inbox
Callee: GET /v1/a2a/inbox?wait= → ack → ComplianceExecutor → reply
Caller: GET /v1/a2a/tasks/{id} 取结果
```

### 3.2 主要端点

| 方法 | 路径 | 发起方 |
|------|------|--------|
| POST | `/v1/a2a/tasks` | Caller Node |
| GET | `/v1/a2a/inbox?wait=` | Callee Node（long poll） |
| POST | `/v1/a2a/tasks/{id}/ack` | Callee |
| POST | `/v1/a2a/tasks/{id}/reply` | Callee |
| GET | `/v1/a2a/tasks/{id}` | Caller / Callee |
| POST | `/v1/a2a/tasks/{id}/caller_notify` | Caller（HITL 中继） |
| POST | `/v1/a2a/tasks/{id}/caller_resume` | Caller |
| GET | `/v1/a2a/tasks/{id}/caller_input?wait=` | Callee |

**Manage 实现**：`manage/a2a/routes.py`、`manage/a2a/store.py`、`manage/a2a/models.py`  
**Node 客户端**：`node/internal/a2aclient/client.go`  
**工具**：`node/internal/tools/tool_a2a.go`

---

## 4. Callee 路径：ComplianceExecutor

### 4.1 Inbox → Turn

**文件**：`node/internal/manage/compliance_executor.go`

```text
InboxPoller 收到 Task
  → ComplianceExecutor.Execute(ctx, task)
  → session.RunInboxTurn(ctx, inboxSessionID, userContent, ...)
  → 本地 turn loop（与 Client 路径共用 runtime 语义）
  → reply 成功 / requires_input（HITL）
```

### 4.2 RunInboxTurn 要点

**文件**：`node/internal/session/a2a_inbox.go`

| 要点 | 说明 |
|------|------|
| 专用 inbox session | 与 Client session 隔离 |
| SSE 订阅 | 入队**前** `afterSeq := hub.CurrentSeq()`，再 `Subscribe(afterSeq)` |
| 多步 HITL | 循环等待 `hitl_required`（inbox 本地 turn）；caller 中继仍为 `approval_required` / `user_information_required` |
| `requires_input` | reply 载荷含 `callee_agent_*` 元数据供 caller 展示 |

### 4.3 跟读清单

1. `inbox_poller.go` — long poll 入口  
2. `compliance_executor.go` — 调度 RunInboxTurn  
3. `a2a_inbox.go` — turn + HITL 等待  
4. `a2aclient/client.go` — ack / reply / caller_input  

---

## 5. Caller 路径与 HITL 中继（v0.3.9）

当 callee 工具需审批（如 `bash_run=always` 在 callee 侧为 ask）：

```text
Callee reply: status=requires_input, payload=approval_required + callee_agent_*
Caller turn: 合成 a2a_relay 工具块 → SSE 到 caller Client TUI
User 审批 → Caller POST caller_resume → Manage
Callee: GET caller_input → RunInboxTurn(resume) → 继续 turn → final reply
Caller: 合成 done / 终态工具块（不等本地 tool_result）
```

**Node 源码**：

| 组件 | 文件 |
|------|------|
| Caller HITL Bridge | `node/internal/session/a2a_caller_hitl.go` |
| requires_input 编码 | `compliance_executor.go`、`encodeRequiresInputPayload` |

**Client 展示**：

| Client | 文件 |
|--------|------|
| Python TUI | `app/cli/tui/app.py`、`child_agent.py` |
| Go TUI | `client/internal/hitl/a2a.go`、`a2a_relay_tools.go` |

**Manage 状态**：`caller_notified` → `caller_responded` → callee 取 `caller_input`

---

## 6. 配置要点

```yaml
manage:
  enabled: true
  url: http://127.0.0.1:8020
  a2a:
    enabled: true          # false = 仅注册，不拉 inbox（纯 caller）
    inbox_wait_seconds: 25
expose_to_peers: true      # 被调方须 true
```

工具组：`tools.enabled_groups` 含 `a2a` 才暴露 `agent_invoke` / `agent_discover`。

---

## 7. 联调案例

**路径**：`cases/a2a-manage-docker/`

```bash
cd cases/a2a-manage-docker
docker compose up --build -d
./scripts/verify-bash-hitl.sh   # 自动化部分路径
# node-b TUI 人工审批中继
```

案例 README 含 node-a（caller）、node-b（callee + bash 审批）拓扑。

---

## 8. 测试索引

```bash
go test ./node/internal/session/a2a_* ./node/internal/manage/compliance_* ./node/internal/a2aclient/...
go test ./client/internal/hitl/... ./client/internal/tui/full/a2a_*
pytest tests/test_cli_a2a_relay.py tests/test_manage_a2a_store.py -q
```

---

## 9. 源码索引

| 概念 | 路径 |
|------|------|
| Manage 入口 | `manage/manage_app.py` |
| A2A store | `manage/a2a/store.py` |
| Node 注册 | `node/internal/manage/registrar.go` |
| Inbox | `node/internal/manage/inbox_poller.go` |
| Callee 执行 | `node/internal/manage/compliance_executor.go` |
| Inbox turn | `node/internal/session/a2a_inbox.go` |
| Caller 中继 | `node/internal/session/a2a_caller_hitl.go` |
| A2A HTTP 客户端 | `node/internal/a2aclient/client.go` |

---

## 10. 下一章

→ [06-运维与案例](./06-运维与案例.md)：开发栈、打包、OS 兼容、案例索引。
