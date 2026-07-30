# 工作组协作 + Node↔Manage 长连接（设计）

> **分支**：`cursor/remote-agent-placement-7e3e`  
> **状态**：方案讨论中（未开工实现）  
> **范围**：重构 A2A 协作模型；统一 Node↔Manage 实时信令；清理轮询/进程级角色遗留  
> **非目标（本设计）**：键鼠远程控制；浏览器直连 home；用 Placement Edge 冒充协作信道

---

## 0. 已拍板（2026-07-30）

| 项 | 决策 |
|----|------|
| Agent ↔ 工作组 | **一对一**：一个 Agent 只属于一个工作组 |
| `@` 语义 | **同步交接**：当前对话 pause，交给目标 Agent 跑完再回来 |
| browse Leader | **同组即允许**，记审计即可，不要二次 HITL |

---

## 0.1 架构分叉：谁跑 LLM loop？

### 方案 A — Node 编排（初稿，**不推荐**）

```text
用户 ↔ Node(Leader LLM+工具) --@--> Manage 仅路由 --→ Node(Member LLM+工具)
```

协作状态散落多 Node；同步 `@` 要跨机卡住等结果；与 Placement/本地 turn 缠在一起 → **心智最复杂**。

### 方案 B — Manage 编排（**推荐，采纳讨论结论**）

```text
独聊（不跨 Agent）
  用户 ↔ Node：LLM + 工具（保持简单）

工作组 / 跨 Agent（必须建组）
  用户 ↔ Node UI（建议 Node 作 BFF）
        → Manage：工作组状态 + Leader/成员 LLM loop + 同步 @
        → 工具 RPC → 各 agent 的 home Node 执行
```

| 职责 | Manage | Node |
|------|--------|------|
| 工作组 / leader / 成员 | ✅ 真源 | 缓存 |
| 组会话 + 同步 `@` | ✅ | 不持有组回合状态 |
| 组内 LLM loop | ✅ | ❌ |
| 工具执行 | 调度 | ✅ 按 home `agent_id` |
| 独聊 | ❌ | ✅ |
| Placement / Edge | 控制面+隧道 | home 实例 |
| 旧 `agent_invoke` | **废除**；跨 Agent **必须**经工作组 | — |

**一句话：** Node = 本机独聊运行时 + 组协作的工具执行器；Manage = 工作组大脑；**没有工作组就没有跨 Agent A2A。**

### 会不会更复杂？

- **整体分支变少**：禁止 Node 侧第二套跨 Node 编排。  
- **复杂收拢到 Manage**：同步交接、权限、审计只实现一次。  
- **Node 变窄**：WS 上主要是 `tool.execute` / HITL / announce。  
- **代价**：Manage 变重（LLM、组会话、关键路径）；Key/模型来源、HITL、UI BFF 要产品说清（见 §10）。

复杂度从「多 Node 隐性双轨」变成「Manage 显性集中」——更可控。

### 权威上下文（方案 B）

组生命周期内，Leader 主对话真源在 **Manage 组会话**；`@` 近 10 条与 `browse_leader_context` 都读 Manage，不必为切片再跨 Node。

---

## 1. 决策摘要（方案 B）

| 项 | 决策 |
|----|------|
| 协作单元 | **工作组** ≠ `discovery_group` |
| 成员 | Agent 实例；一 Agent 一组 |
| 跨 Agent | 必须经工作组 |
| `@` | Manage 内同步交接 + 近 ≤10 条 |
| browse | 同组读 Manage 上 Leader 组会话 |
| 传输 | 每 Node 一条 WebSocket（信令 + tool RPC） |
| 独聊 | 仍在 Node |
| Placement | 独立（住哪台机器 ≠ 谁编排对话） |

---

## 2. 背景

现网 A2A：Node 级信箱 + HTTP 轮询，无实例寻址、无组、无同步交接。方案 B 用 Manage 组会话 + WS 工具 RPC 替换。

---

## 3. 术语

| 术语 | 含义 |
|------|------|
| **WorkGroup** | Manage 协作组织 |
| **组会话** | Manage 持有的组内消息真源 |
| **独聊** | 无跨 Agent 时 Node 本地主对话 |
| **discovery_group** | Placement 用 Node 分区 |
| **工具 Worker** | Node 按 `agent_id` 执行工具，不跑组 LLM |

第一版：成员 home Node 须满足 discovery 可见性（或同 Node）。

---

## 4. 工作组模型

### 4.1 数据（Manage）

```text
WorkGroup {
  group_id, name, leader_agent_id
  members[]: { agent_id, home_node_id, display_name }
  session: { messages[], assign_stack[] }   # 组会话
}
```

### 4.2 HTTP 控制面

CRUD、加人/移出、选 leader；实例仍先本地或 Placement 创建，再入组。

### 4.3 同步 `@`

1. Manage 跑 Leader LLM；工具 → Leader home Node  
2. 遇 `@`：pause Leader，开 Member 回合（instruction + 近 ≤10 条），Member 工具 → Member home  
3. Member 结束 → 结果写入组会话 → resume Leader  

### 4.4 browse

Member 工具读 Manage 组会话；同组鉴权；审计。

---

## 5. 工具

| 工具 | 谁 | 哪里执行 |
|------|----|----------|
| `assign_workgroup_task` | Leader | Manage 编排器内 |
| `browse_leader_context` | Member | Manage 读组会话 |
| FS/Shell/Browser… | 当前回合 Agent | home Node via WS |

旧 `agent_invoke` / `agent_discover`：主路径删除。

---

## 6. WebSocket

`hello` / `ping` / `node.announce` / `tool.execute|result|need_hitl`；可选 `ui.proxy`。  
删除 inbox long poll、task 短 poll、caller_input wait、HTTP 心跳。

---

## 7. Placement

解决「Agent 住哪」；工作组解决「谁协作、谁跑 LLM」。远端 Agent：工具在 home，组会话在 Manage。

---

## 8. 过时逻辑

| 项 | 动作 |
|----|------|
| Node 级 invoke/discover | 废除 |
| InboxPoller / 临时 a2a session | 删除 |
| Node 实现 `@`/browse | 不实现 |
| 「Manage 不跑 turn」 | **修订**：独聊不跑；**组回合 Manage 跑** |

---

## 9. 分期

| Phase | 内容 |
|-------|------|
| D0 | 拍板 B（本文） |
| D1 | WorkGroup HTTP + UI |
| D2 | Node WS + tool worker |
| D3 | 组会话 + Leader loop + 同步 `@` |
| D4 | Member loop + browse |
| D5 | UI BFF 组聊；拆旧 A2A |
| D6 | LLM 配置 / HITL / HA |

---

## 10. 开工前仍待确认

1. 组会话 LLM：Manage 全局模型，还是跟随 Leader 在 Node 的绑定档案（Manage 代调）？  
2. UI：组聊经 **Node BFF**（建议）还是浏览器直连 Manage？  
3. 入组后：组会话从空开始，还是迁移原 Node 独聊历史？  

---

## 11. 相关锚点

- Placement：`remote-agent-placement.md`  
- 清理清单：`node-centric-architecture-cleanup.md`  
- 实例模型：`agent-instance-model.md`  
- 旧「Manage 不跑 turn」：`manage-architecture.md`（将被修订）
