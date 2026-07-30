# 工作组协作 + Node↔Manage 长连接（设计）

> **分支**：`cursor/remote-agent-placement-7e3e`  
> **状态**：设计冻结草案（先方案后实现）  
> **范围**：重构 A2A 协作模型；统一 Node↔Manage 实时信令；清理轮询/进程级角色遗留  
> **非目标（本设计）**：键鼠远程控制；浏览器直连 home；用 Placement Edge 冒充协作信道

---

## 1. 决策摘要

| 项 | 决策 |
|----|------|
| 协作单元 | 新增 **工作组（WorkGroup）**，与 Placement 用的 `discovery_group` **分离** |
| 成员粒度 | 工作组成员 = **Agent 实例**（`agent_id`），挂在某一 `home_node_id` 上 |
| Leader | 每组恰好 **1 个 leader agent**；可改选同组成员 |
| Leader 能力 | 持有组内「权威上下文」；工具 **`@`（assign）** 向同组其他 Agent 派任务 |
| `@` 载荷 | 任务说明 + **最近最多 10 条** leader 消息（结构化摘要，非全量 dump） |
| 成员能力 | 新工具 **`browse_leader_context`**：只读浏览 leader 上下文（可分段/检索） |
| Node↔Manage | **一条长连接（WebSocket）/ Node** 统一信令；废弃 inbox long poll、task 结果短 poll、caller_input long poll、注册心跳 HTTP |
| 控制面 HTTP | 保留：工作组 CRUD、成员移动、选 leader、只读查询；热路径走长连接 |
| Placement / Edge | **保留独立**：远端创建/双删/会话代理仍走 Control + Edge；不与工作组信令混用同一语义帧（可同物理 WS 多路复用，见 §6） |

---

## 2. 背景：为何现网 A2A 不够用

当前 A2A 是「**Node 级** Task 信箱 + HTTP long poll / 短 poll」：

- `agent_invoke` / `agent_discover` 目标是 **node_id**，无法指到用户 Agent 实例
- Callee 开临时 `a2a-{task}` session，**与用户主对话脱节**
- 无「组 / leader / 共享上下文」；模型只能把全文塞进 `content`
- Node↔Manage：inbox wait、结果 2s 轮询、caller_input wait、30s 心跳——延迟高、语义碎

目标模型：

```text
工作组 G
  leader: Agent L（完整上下文）
  members: Agent M1, M2, …
  L --[@ 派任务 + 近 10 条]--> M*
  M* --[browse_leader_context]--> 只读 L 上下文
```

跨 Node 时，消息经 **Manage 长连接** 路由到 home Node，再投递到具体 `agent_id` runtime。

---

## 3. 术语与边界

| 术语 | 含义 |
|------|------|
| **WorkGroup** | 协作组织单位；有 `group_id`、名称、成员列表、`leader_agent_id` |
| **成员** | `{ agent_id, home_node_id, display_name, role: leader\|member }` |
| **discovery_group** | **Placement / 同组放置** 用的网络可见性分区（Console 分配给 Node）；**不是**工作组 |
| **Placement** | 在同 discovery_group 的其他 Node 上创建 Agent；与工作组正交：工作组可跨 Node，但成员须已存在（本地或远端引用） |
| **权威上下文** | Leader Agent 主对话的 messages / transcript（真源在 **leader 所在 home Node**） |

**正交关系：**

```text
discovery_group（Node 级，放置/peers）
       ≠
WorkGroup（Agent 实例级，协作）

同一 discovery_group 内的多个 Node 上的 Agent，可加入同一 WorkGroup。
不同 discovery_group 的 Node 默认不可互相 Placement；WorkGroup 是否允许跨 discovery_group：
  → 第一版 **禁止**（成员 home 的 Node 须与创建者同属至少一 discovery_group 交集，或同 Node）。
```

---

## 4. 工作组模型

### 4.1 数据（Manage 为目录真源）

```text
WorkGroup {
  group_id
  name
  description?
  created_by_node_id
  leader_agent_id          # 必填；改选时事务更新
  members[]: {
    agent_id
    home_node_id
    display_name
    joined_at
  }
  created_at, updated_at
}
```

Node 本地可缓存「本机 agent 所属工作组」视图（由 WS 推送同步），避免每次工具调用打 HTTP。

### 4.2 API（控制面，HTTP）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/workgroups` | 创建组；可同时指定初始成员 + leader |
| GET | `/v1/workgroups` | 列表（按调用 Node/Agent 可见性过滤） |
| GET | `/v1/workgroups/{id}` | 详情 |
| PATCH | `/v1/workgroups/{id}` | 改名等 |
| DELETE | `/v1/workgroups/{id}` | 解散 |
| POST | `/v1/workgroups/{id}/members` | 加入：新建绑定或移动已有 agent |
| DELETE | `/v1/workgroups/{id}/members/{agent_id}` | 移出 |
| POST | `/v1/workgroups/{id}/leader` | `{ agent_id }` 改选 leader（须已是成员） |

**创建 Agent 并入组：**

- UI：创建向导增加「加入工作组 / 设为 leader」；先 `POST /v1/agents`（本地或 Placement），再 `POST .../members`
- 或 Manage 提供组合接口（可选）：内部串上述两步

**移动已有 Agent：**

- 仅改工作组隶属，不改 `home_node_id` / Placement
- 一个 Agent **第一版只属于一个工作组**（简化工具与权限）；后续再开多组

### 4.3 Leader 规则

1. 创建组时必须指定 leader；若只建空组，则第一个加入的成员自动成为 leader  
2. 改选：原 leader 降为 member；新 leader 须在组内  
3. Leader 删除/移出前必须先改选或解散  
4. Leader 的「完整上下文」= 其主对话 runtime（**不是**另建组级影子 session）

---

## 5. 工具设计

### 5.1 `@` / `assign_workgroup_task`（仅 Leader）

**注册名（建议）：** `assign_workgroup_task`  
**别名提示：** 系统提示中说明可用 `@成员名` 语义，工具参数仍用结构化字段。

```json
{
  "group_id": "wg_...",
  "to_agent_id": "agt_...",
  "instruction": "请审查模块 X 并给出结论",
  "include_recent_messages": 10
}
```

**行为：**

1. 校验：调用方 `agent_id` == 该组 `leader_agent_id`；`to_agent_id` 为同组成员且非自己  
2. 从 **leader runtime** 取最近 `min(N,10)` 条消息，做成紧凑结构（role + 截断 content；工具结果可压缩为摘要）  
3. 经 Manage 长连接投递 `WorkgroupAssign` 到 `to` 的 home Node  
4. 目标 Agent **入队到其主对话**（或标记为「工作组任务」高优先级 human 消息），携带：
   - instruction
   - recent_messages（≤10）
   - from_leader / group_id / assign_id  
5. 返回 `assign_id`；完成态经 WS 回推 leader（可选等结果 / 异步通知）

**不做：** 把 leader 全量上下文塞进一次工具调用（成本爆炸）；全量靠成员侧 `browse_leader_context`。

### 5.2 `browse_leader_context`（仅非 Leader 成员）

```json
{
  "group_id": "wg_...",
  "mode": "recent" | "range" | "search",
  "limit": 20,
  "before_seq": null,
  "query": null
}
```

**行为：**

1. 校验：调用方是该组成员且不是 leader  
2. Manage 向 leader home Node 请求 **只读上下文切片**（经 WS RPC）  
3. home 从 leader runtime/store 读 messages，按 mode 返回（默认最近 N，单条有 rune 上限）  
4. **禁止**成员写入 leader 上下文；**禁止**键鼠/屏幕（与 Placement 旁观无关）

### 5.3 工具可见性

| Agent 身份 | `assign_workgroup_task` | `browse_leader_context` | 旧 `agent_invoke` |
|------------|-------------------------|-------------------------|-------------------|
| 组内 leader | ✅ | ❌ | 废弃或降级为「跨组遗留」 |
| 组内 member | ❌ | ✅ | 同上 |
| 无工作组 | ❌ | ❌ | 过渡期可保留 |

第一版：**同组协作只走工作组工具**；跨组 A2A 若仍需要，另开「跨组邀请」需求，不沿用 node 级 invoke。

### 5.4 系统提示注入

- Leader：说明组内成员列表 + `@`/assign 用法 + 「近 10 条会自动附带」  
- Member：说明可用 `browse_leader_context` 查阅 leader，勿臆造 leader 未展示的内容  

---

## 6. Node↔Manage 长连接

### 6.1 传输

- **WebSocket**：`GET /v1/node/ws`（或 `/v1/gateway/ws`）  
- 每个 Node **一条**连接；鉴权：`node_token` + `node_id`  
- 消息：JSON 帧（可后续换 MessagePack）；支持 request/response（带 `corr_id`）与 server push  

### 6.2 帧类型（初稿）

**连接与存活**

| 方向 | type | 说明 |
|------|------|------|
| C→S | `hello` | node_id、version、capabilities |
| S→C | `hello_ok` | session_id、server_time |
| 双向 | `ping`/`pong` | 替代 HTTP 心跳；Manage 据此刷新 online TTL |

**注册 / 目录（可逐步迁入）**

| 方向 | type | 说明 |
|------|------|------|
| C→S | `node.announce` | 原 register/heartbeat 载荷（name、placement flags、local_agents 摘要） |
| S→C | `workgroup.changed` | 成员/leader 变更，刷新本地缓存 |

**工作组协作**

| 方向 | type | 说明 |
|------|------|------|
| C→S | `wg.assign` | leader 派任务 |
| S→C | `wg.assign_deliver` | 投递到目标 home |
| C→S | `wg.assign_ack` / `wg.assign_result` | 已入队 / 完成摘要 |
| C→S | `wg.browse_leader` | 成员请求 leader 上下文 |
| S→C | `wg.browse_leader_forward` | 转到 leader home |
| C→S | `wg.browse_leader_reply` | home 返回切片 |

**（可选二期）与 Placement 复用**

- 同 WS 上 `edge.*` 隧道帧，逐步替代「每次 HTTP Edge session」；**第一版可不做**，避免与工作组抢范围。

### 6.3 替代的旧轮询清单

| 旧机制 | 处置 |
|--------|------|
| `GET /v1/a2a/inbox?wait=` | **删除**；改为 WS push `wg.assign_deliver` / 通用 `task.deliver` |
| Caller `GET /v1/a2a/tasks/{id}` 2s 轮询 | **删除**；改为 WS `wg.assign_result` / `task.update` |
| `GET .../caller_input?wait=` | **删除或改为 WS RPC** |
| `POST .../heartbeat` 周期 | **删除**；改为 WS ping + `node.announce` |
| HTTP register 周期 upsert | 改为连接后 `node.announce`；断线重连再 announce |

**过渡期：** 可保留 HTTP 只读查询（任务详情、工作组列表）；写路径与投递一律 WS。

### 6.4 可靠性

- 至少一次投递 + `assign_id` 幂等；Node 落盘 outbox/inbox 游标  
- 断线重连：补拉 `since_cursor` 未确认帧  
- Manage 不持有 Agent 对话真源；只做路由与工作组目录  

---

## 7. 与 Placement / Registry 的关系

```text
┌──────────────┐     discovery_group      ┌──────────────┐
│ Node A       │◄────────────────────────►│ Node B       │
│  Agent L     │     Placement/peers      │  Agent M     │
└──────┬───────┘                          └──────┬───────┘
       │         WorkGroup（Manage 目录）         │
       └──────────── leader L + member M ────────┘
                         │
                    WS 长连接信令
```

- **创建远端 Agent**：仍用 Control + `allow_peer_create`  
- **再拉进工作组**：`POST .../members`  
- **聊天/SSE**：远端 stub 仍走 Edge；工作组 assign/browse 走 WS（可经 Manage 转发到 home，home 再写本地 runtime）

---

## 8. 过时逻辑清理（与本设计一并规划）

### 8.1 随工作组上线删除/冻结

| 项 | 动作 |
|----|------|
| Node 级 `agent_invoke` / `agent_discover` 作为主协作路径 | 冻结；文档标 deprecated；工具组 `a2a` 改为 `workgroup` |
| InboxPoller + ComplianceExecutor 临时 `a2a-*` session | 由「投递到目标 **主** Agent runtime」替代 |
| `expose_to_peers` 作为「可被协作」的主开关 | 协作改为「是否在工作组内」；`accept_inbound` 仅保留给「允许被拉进组 / 接受 WS 投递」Node 总闸（可选） |
| ops/compliance 角色叙事 | 已去门控；文档与案例彻底去掉 |
| A2A Task 状态机 HTTP 全家桶（过渡后） | 映射为 WS 帧或缩成 WorkgroupAssign 记录表 |

### 8.2 继续保留但改名/收口

| 项 | 动作 |
|----|------|
| `discovery_group` | **仅** Placement / peers / 可见性 |
| Registry `node_id` | 继续；Console 展示 Node |
| Edge Tunnel | Placement 会话面；与 WS 信令分工 |
| `node-centric-architecture-cleanup.md` 中 P0–P2 | 按该文推进；本设计依赖「实例级寻址」提前到工作组成员键 |

### 8.3 必须新增的前置

- Manage / Node：**实例级** `agent_id` 公告（heartbeat/`node.announce` 的 `local_agents[]`）  
- 投递路由：`agent_id → home_node_id`（Placement owner 引用 + home 实例；Manage 工作组成员表双写）

---

## 9. 分期实现（仍在本分支演进）

| Phase | 内容 | 破坏面 |
|-------|------|--------|
| **D0** | 本文设计冻结；标注旧 A2A 废弃点 | 文档 |
| **D1** | Manage WorkGroup 存储 + HTTP CRUD；Web UI 雏形（建组/成员/选 leader） | 中 |
| **D2** | Node↔Manage **WebSocket** 网关；hello/ping/`node.announce` 替代心跳 | **大** |
| **D3** | `assign_workgroup_task` + 投递到目标主会话 + 近 10 条消息 | 大 |
| **D4** | `browse_leader_context` 只读 RPC | 中 |
| **D5** | 下线 InboxPoller / task 轮询 / 旧 a2a 工具主路径；案例与 handbook 重写 | 大 |
| **D6**（可选） | WS 多路复用 Edge；实例级 A2A 残留清理 | 大 |

**建议落地顺序：** D0（本文）→ D2 与 D1 可并行骨架 → D3/D4 工具闭环 → D5 拆旧。

---

## 10. 开放问题（实现前拍板）

1. Agent 是否允许属于多个工作组？（建议 v1：**否**）  
2. `@` 是否同步等待成员完成，还是只「火后忘」+ 通知？（建议：默认异步，工具返回 `assign_id`，可选 `wait=true`）  
3. Leader 上下文 browse 的鉴权：仅同组，还是要二次 HITL？**（建议：同组即允许，审计日志）**  
4. 成员与 leader 不同 Node 时，browse/assign 延迟 SLA 与截断策略  
5. 工作组是否出现在 Manage Console 一等导航？（建议：要）

---

## 11. 相关锚点

| 主题 | 路径 |
|------|------|
| 实例模型 | `docs/design/agent-instance-model.md` |
| Placement | `docs/design/remote-agent-placement.md` |
| Node 开关清理 | `docs/design/node-centric-architecture-cleanup.md` |
| 现网 A2A | `docs/manage-communication.md`、`manage/a2a/`、`node/internal/manage/inbox_poller.go` |
| 近 10 条上下文 API | `GET /v1/agents/{id}/context`（可复用切片逻辑给 `@`） |
| 工具 | `node/internal/tools/tool_a2a.go` → 将演进为 `tool_workgroup.go` |

---

## 12. 一句话

用 **工作组 + Leader 权威上下文 + `@`（附带近 10 条）+ 成员浏览 Leader** 替换 Node 级 invoke；用 **每 Node 一条 WebSocket** 替换 inbox/结果/心跳轮询；**discovery_group 只服务 Placement**，不再冒充协作组。
