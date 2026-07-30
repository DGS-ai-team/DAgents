# 工作组协作 + Node↔Manage 长连接（设计）

> **分支**：`cursor/remote-agent-placement-7e3e`  
> **状态**：方案讨论中（未开工实现）  
> **推荐方向**：Manage 编排组会话 + 隐式云 Leader；Node 独聊 + 工具执行 + 组聊订阅  

---

## 0. 已拍板

| 项 | 决策 |
|----|------|
| Agent ↔ 工作组 | **一对一**（成员 Agent）；一个成员 Agent 只属于一个工作组 |
| `@` | **同步交接**（pause → 成员跑完 → resume） |
| browse Leader | **同组即允许**，审计即可 |
| Leader | **隐式云 Agent**：随工作组/组会话创建而创建，**不是**某 Node 上已有 Agent |
| Leader / 组会话 LLM | **仅 Manage 云配置**；Manage 维护**独立** LLM 配置清单（与 Node 档案分离） |
| 成员来源 | **只支持新建**；**不支持**把已有本地/远端 Agent「拉进组」 |
| 入组历史 | **不存在**迁移问题；组会话随 Leader 从空开始 |
| 组聊可见性 | 任意 Node 可 **订阅** 组会话：实时看消息、参与 HITL、以不同 `human_name` 发消息 |
| UI | `/ui` 左侧与本地 Agent 清单 **分栏**：本地 Agents ‖ 已订阅工作组 |

---

## 1. 合适性结论（对本版）

**合适，且比「本地 Agent 当 Leader / 拉已有 Agent 入组」更干净。**

| 优点 | 说明 |
|------|------|
| 身份不拧巴 | Leader 天生属于 Manage，不再纠结「哪个 Node 上的谁是 Leader」 |
| 编排与配置同地 | 组 LLM loop + LLM 清单都在 Manage，无跨 Node 抢 Key |
| 生命周期简单 | 建组 = 建 Leader + 空会话；解散 = 收掉云 Leader 与组会话 |
| UI 心智清晰 | 本地助手 vs 云协作组，分栏不混 |
| 多 Node 协作 | 订阅模型天然支持「多人多端看同一组聊 + HITL」 |
| 砍掉迁移 | 禁止已有 Agent 入组 → 无 transcript 合并、无归属冲突 |

**需要自觉接受的代价：**

1. Manage 成为组协作的**强依赖**（LLM、会话、订阅扇出）。  
2. 成员 Agent 仍要有 **home Node**（工具/沙箱落地处）——创建成员时必须指定跑在哪台 Node（或走 Placement）。  
3. Leader 作为云 Agent：**默认不应假设有本地 FS**；重活通过 `@` 交给带 home 的成员，或显式绑定「工具执行 Node」（见 §10 开放项）。  
4. 与旧 A2A / 本地 Agent 目录彻底分家——产品上要讲清「本地助手」和「工作组」是两样东西。

**总评：方向正确，建议按此冻结主体；把「成员创建落点」和「Leader 是否绑工具 Node」两点定掉即可开工。**

---

## 2. 概念模型

```text
Manage
  LLM 配置清单（云）
  WorkGroup
    ├── LeaderAgent（隐式，云，随组创建）
    ├── GroupSession（消息真源，Manage）
    ├── MemberAgents[]（新建 only，各有 home_node_id）
    └── Subscribers[]（node_id / 操作者，可多 Node）

Node
  本地 Agents（独聊，LLM+工具）—— 与工作组无关
  订阅的工作组（UI 分栏）—— 看组聊 / HITL / 发 human 消息
  工具 Worker —— 执行「落在本机 home 的成员」的 tool_call
```

### 2.1 三角色

| 角色 | 是什么 | 做什么 |
|------|--------|--------|
| **Leader（云）** | 组隐式 Agent | Manage 上跑 LLM；`@` 同步交接；持有组会话权威上下文 |
| **Member（新建）** | 组内工作 Agent | 被 `@`；可 `browse_leader_context`；工具在 **home Node** |
| **Subscriber（人/端）** | 订阅了该组的 Node 会话 | 实时组聊、HITL、以 `human_name` 发言；**不是**组内 Agent |

本地 Agent 清单里的实例 **不会**变成 Leader，也 **不能**直接变成成员。

### 2.2 与 discovery_group / Placement

- `discovery_group`：仍只服务 Placement / peers（Agent 能建在哪台 Node）。  
- 创建 **Member** 时：在 UI 选 home Node（本机或同 discovery_group 可 Placement 的 Node）→ 新建实例 → 写入工作组成员表。  
- Leader：**不**占用 Placement，不住在某 Node。

---

## 3. 生命周期

### 3.1 创建工作组

1. 调用方（某 Node 已登录/已连 Manage）`POST /v1/workgroups`  
2. Manage：创建 WorkGroup + **隐式 LeaderAgent** + 空 GroupSession  
3. 选用 Manage LLM 清单中的档案（创建时指定或用默认）  
4. 创建者所在 Node **自动订阅**该组  

### 3.2 新建成员（唯一加人方式）

1. `POST /v1/workgroups/{id}/members`：`display_name` + `home_node_id` + 模板/默认工具策略等  
2. Manage 经 Control/Placement 或内部 RPC 在 home Node **新建** Agent 实例  
3. 记入成员表；该 Agent **不可**再加入其他组；**不可**从「已有 Agent」迁入  

### 3.3 解散

解散组 → 归档/删除组会话与云 Leader；成员实例策略：**默认归档或删除 home 上实例**（实现期二选一，建议默认归档）。

---

## 4. 组会话与同步 `@`

- 组消息真源在 Manage。  
- Leader 回合：Manage 用**云 LLM 清单**调用模型。  
- Leader 若发出工具调用：  
  - 元工具（`@`、列成员）在 Manage 内完成；  
  - 若允许 Leader 绑「工具 Node」（可选），再 WS `tool.execute`；**v1 可禁止 Leader 本地工具，只允许 `@`**。  
- `@`：同步 pause Leader → Member 回合（instruction + 近 ≤10 条组消息）→ Member 工具打到其 home Node → 结果回写 → resume Leader。  
- `browse_leader_context`：Member 读 Manage 组会话。

---

## 5. 订阅、HITL、发消息

### 5.1 订阅

- Node 经 WS / HTTP：`subscribe(group_id)` / `unsubscribe`  
- 订阅后收：`session.message`、`hitl.request`、`assign.*` 等推送  
- 多 Node 可同时订阅同一组（扇出）

### 5.2 Human 消息

- `POST` 或 WS：`human_name` + `text`（或多模态后续）  
- 写入组会话，role=user，带 `human_name` / `from_node_id`  
- 不同订阅端用不同 human_name，便于区分操作者  

### 5.3 HITL

- 成员（或 Leader 若有工具）在 home Node 触发审批 → Node → Manage → **所有订阅端**可见并可由任一端应答（需幂等：一笔 HITL 只接受一次 resolve）

### 5.4 UI（`/ui`）

```text
左侧
  ┌ 本地 Agents ─────┐
  │ Agent A          │
  │ Agent B          │
  ├ 工作组（已订阅）──┤
  │ WG 「发布评审」   │  ← 点进组聊，不是本地 Agent 页
  │ WG 「客服值班」   │
  └──────────────────┘
```

组聊页：时间线 + HITL + 输入框（选 human_name）+ 成员/Leader 状态；不与单 Agent 独聊页混用同一 store。

---

## 6. Node↔Manage 长连接

每 Node 一条 WebSocket：

| 帧 | 用途 |
|----|------|
| hello / ping / announce | 存活与目录 |
| workgroup.subscribe fanout | 组事件 |
| tool.execute / result / need_hitl | 成员（及可选 Leader）工具 |
| human.message / hitl.resolve | 订阅端输入 |

废除：旧 A2A inbox 轮询、task 短 poll、HTTP 心跳作主路径。

---

## 7. Manage 云 LLM 清单

- Manage 设置/Console：独立 `llm_profiles`（与 Node `llm_configs.db` 分离）  
- 工作组创建时绑定 `llm_profile_id`；可改绑  
- Key 只存 Manage 侧；Node 独聊仍用 Node 档案  

---

## 8. 明确不做

- 已有 Agent 加入/移入工作组  
- 本地 Agent 升为 Leader  
- 无工作组的跨 Agent `agent_invoke`（主路径）  
- 把 Node 独聊历史合并进组会话  

---

## 9. 分期

| Phase | 内容 |
|-------|------|
| D0 | 本文主体冻结（讨论收口） |
| D1 | WorkGroup + 隐式 Leader + Manage LLM 清单 + HTTP |
| D2 | Node WS + 订阅扇出 + UI 分栏只读组聊 |
| D3 | Human 发言 + HITL 多端 |
| D4 | 新建 Member（指定 home）+ 同步 `@` + browse |
| D5 | 拆旧 A2A；文档/案例 |

---

## 10. 开工前最后开放项

1. **Leader v1 是否允许任何 Node 侧工具？**  
   - 建议 **否**：Leader 只编排 + `@`；执行一律 Member。  
2. **新建 Member 失败（home 离线）时**：组仍建成功、成员挂 pending，还是整笔失败？  
3. **解散组时成员实例**：归档 vs 删除。  
4. **订阅ACL**：同 discovery_group 任意 Node 可订，还是要邀请码/白名单？  

---

## 11. 一句话

工作组 = Manage 上的云协作房间：隐式云 Leader + 云 LLM + 组会话；成员只能新建并落在某 Node 上跑工具；各 Node 通过订阅参与组聊/HITL；本地 Agent 与工作组分栏、互不掺和。
