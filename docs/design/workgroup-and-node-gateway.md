# 工作组协作 + Node↔Manage 长连接（设计）

> **分支**：`cursor/remote-agent-placement-7e3e`  
> **状态**：方案讨论中（主体倾向冻结）  
> **推荐方向**：Manage 云 Leader（Supervisor）+ 总消息列表；Node 真实环境工具执行；统一权限模块  

---

## 0. 已拍板

| 项 | 决策 |
|----|------|
| 跨 Agent | **必须**经工作组；废除 Node 级 `agent_invoke` 主路径 |
| Leader | **隐式云 Agent**，随工作组创建；**Manage 云 LLM 清单** |
| Leader 职责 | **Supervisor**：收用户消息 → 计划 → 编排成员；**仅编排类工具** |
| `@` | **同步交接**；成员产出写入**总消息列表**并标注身份 |
| 成员 | **只新建**；不支持已有 Agent 入组；一成员一工作组 |
| 执行环境 | **去掉产品内沙箱**；真实环境执行。要隔离就 **单独机器部署 Node** |
| 组聊 UI | `/ui` 与本地 Agent **分栏**；多 Node **订阅**后可见/HITL/发言 |
| 订阅 | **非开放**；纳入统一权限模块（见 §8） |

---

## 1. 合适性（含本轮变更）

| 变更 | 评价 |
|------|------|
| 去掉沙箱 | **合适**。隔离用「独立 Node/机器」表达，比 Docker/remote sandbox 叠在架构里更直观，也少一条与 Placement 混淆的轴。 |
| Leader = Supervisor + 仅编排工具 | **合适**。与「Manage 跑 LLM、Node 跑工具」一致；Leader 不碰 FS/Shell。 |
| 总消息列表 + 身份标注 | **合适**。组会话唯一时间线，订阅端与模型看到的是同一本账。 |
| 重构权限 | **必要**。现网 `admin/member/node` + `discovery_group` 不够表达「谁能订组/谁能 HITL/谁能建成员」。 |

代价：Manage 更重；无沙箱后误操作面 = 真实机——靠 **选对 home Node / 独立机器** 与 **工具策略（后续可做 allowlist）** 收敛，而不是容器层。

---

## 2. 去掉沙箱后的执行模型

```text
不要：  Agent.sandbox.enabled / docker / remote sandbox 作为产品主路径
要：    成员 Agent 在指定 home Node 的真实 FS / Shell / Browser 上执行
隔离：  部署一台「干净」机器跑 Node，把高风险成员建在那台 Node 上
```

| 旧概念 | 处置 |
|--------|------|
| `sandbox.enabled` / Docker 后端 | **产品下线**（设计层废弃；实现期分阶段删 UI → API → 代码） |
| `sandbox.backend=remote` | 已与 Placement 划清；一并废弃 |
| Placement | **保留**：只表示「成员实例住在哪台 Node」 |
| 本地独聊 Agent | 同样默认真实环境；不再引导「开沙箱」 |

创建成员时仍选 `home_node_id`（本机或可 Placement 的 Node）——这是 **落点**，不是沙箱开关。

---

## 3. 概念模型

```text
Manage
  ├── 权限与主体（User/Operator、Node、授权）     §8
  ├── 云 LLM 清单
  └── WorkGroup
        ├── Leader（云 Supervisor，仅编排工具）
        ├── Transcript[]（总消息列表，带身份）     §5
        ├── Members[]（新建，home_node_id，真实环境执行）
        └── ACL / 订阅关系

Node
  ├── 本地 Agents（独聊，真实环境）
  ├── 已授权订阅的工作组（UI 分栏）
  └── 工具 Worker（执行本机 home 的成员 tool_call）
```

### 3.1 角色

| 角色 | 说明 |
|------|------|
| **Leader** | Supervisor：收 human 消息、做计划、`@` 成员、维护总时间线语义 |
| **Member** | 被编排的执行者；工具在 home Node **真实环境**；产出消息进总列表 |
| **Subscriber / Operator** | 经 ACL 订阅的人端：看时间线、HITL、以 `human_name` 发言 |

---

## 4. Leader = Supervisor（仅编排工具）

Leader **不在**任何 Node 上执行 Shell/FS/Browser。

**允许的工具（Manage 内建）：**

| 工具 | 作用 |
|------|------|
| `assign_workgroup_task`（`@`） | 同步交接给成员；附带近 ≤10 条总列表摘要 |
| `list_workgroup_members` | 列成员与 home/状态 |
| `browse_member_status`（可选） | 看某次 assign 进度 |
| （可选后续）`cancel_assign` | 取消未完成交接 |

**不做：** `bash_run`、读盘、浏览器等——一律 `@` 给成员。

循环：

```text
Human(s) → 总列表
    → Leader 计划
        → @ Member（同步）
            → Member 回合（工具在 home Node）
            → Member 产生的 assistant/tool/user 片段写入总列表（带身份）
        → Leader 继续或再 @
```

---

## 5. 总消息列表（Group Transcript）

Manage 维护 **唯一** `Transcript[]`，订阅端与 Leader/Member 上下文都由此投影。

### 5.1 条目形状（示意）

```json
{
  "seq": 1042,
  "ts": "2026-07-30T12:00:00Z",
  "role": "assistant",
  "kind": "agent_message",
  "actor": {
    "type": "leader" | "member" | "human" | "system",
    "id": "lead_xxx" | "agt_xxx" | "op_xxx",
    "display_name": "发布评审 Leader" | "代码员" | "Alice",
    "home_node_id": null | "node-b",
    "human_name": null | "Alice"
  },
  "assign_id": null | "asg_...",
  "content": "...",
  "content_parts": [],
  "tool_call_id": null,
  "visibility": "all"
}
```

### 5.2 写入规则

| 来源 | 如何进总列表 |
|------|----------------|
| 订阅端 human | `actor.type=human`，带 `human_name` + `from_node_id` |
| Leader 输出 | `actor.type=leader` |
| `@` 开始/结束 | `system` 或带 `assign_id` 的边界标记 |
| Member 回合中的消息 | **全部拼接**进总列表，`actor.type=member` + `actor.id` + `display_name`；工具调用/结果也入库（可对 UI 折叠） |
| HITL | 请求与决议都进列表，标 human/system |

Member 本地若有 transient 日志，**不以**本地独聊为准；组协作可见性以 Manage 总列表为准。

### 5.3 给模型的视图

- Leader：总列表（可压缩策略另议）+ 成员名册  
- Member 被 `@`：instruction + 总列表近 ≤10 条（已含身份字段）  
- `browse_leader_context`：读总列表切片（同组 ACL）  

---

## 6. 生命周期（摘要）

1. **建组** → 隐式 Leader + 空 Transcript + 创建者获 **wg_owner** + 自动订阅  
2. **新建成员** → 指定 `home_node_id`，在真实环境 Node 上建实例（无 sandbox 字段）  
3. **订阅** → 仅 ACL 允许的 Operator/Node（§8）  
4. **解散** → 归档 Transcript + Leader；成员实例默认归档  

---

## 7. Node↔Manage 长连接

每 Node 一条 WebSocket：`hello` / `ping` / `announce` / 组事件扇出 / `tool.execute|result|need_hitl` / human&HITL。  
废除旧 A2A 轮询热路径。

---

## 8. 权限模块重构

### 8.1 现状问题

现网大致是：

- `MANAGE_TOKENS`：`admin` | `member` | `node` + `discovery_groups`  
- Node：`node_token` + `x-dagents-agent-id`（实为 node_id）  
- `discovery_group` 兼作 Placement 与可见性  

不足以表达：工作组所有者、谁能订阅、谁能 HITL、谁能在某 Node 建成员、云 LLM 谁能用。

### 8.2 主体（Principal）

| 类型 | 标识 | 说明 |
|------|------|------|
| `platform_admin` | token / 账号 | Manage Console 全局 |
| `node` | `node_id` | 机器；WS 鉴权；只对自己 home 的成员执行工具 |
| `operator` | `operator_id` | **人**；经 Node UI 登录/绑定后带入请求（可先用「node 本地操作者配置」过渡） |
| `workgroup_leader` | `leader_id` | 云 Supervisor（系统主体，不用于登录） |
| `workgroup_member` | `agent_id` | 成员实例 |

v1 可让 `operator` 弱绑定为 `(node_id, human_name)`，但权限表按 `operator_id` 设计，避免永远绑死在 node 上。

### 8.3 资源与动作

| 资源 | 动作 |
|------|------|
| `platform` | 管理 LLM 清单、全局 Node、审计 |
| `node` | 注册、announce、在其上 **create_member**、执行 tool |
| `workgroup` | create / delete / update / manage_members / manage_acl |
| `workgroup.transcript` | read（订阅流） / append_human / resolve_hitl |
| `workgroup.assign` | 由 Leader 发起（系统）；成员执行 |

### 8.4 工作组内建角色（RBAC）

| 角色 | 权限 |
|------|------|
| **wg_owner** | 管理 ACL、建/删成员、解散组、订阅、发言、HITL |
| **wg_collaborator** | 订阅、发言、HITL；**不能**改 ACL/解散 |
| **wg_viewer**（可选） | 只订阅只读；不能发言/HITL |

- 建组者 → `wg_owner`  
- **禁止**「同 discovery_group 自动可订」——Placement 组网 ≠ 协作授权  
- 邀请：owner 将 `operator_id` 或 `node_id`（过渡）加入 ACL 后，方可 `subscribe`  

```text
WorkGroupACL {
  owners: [principal…]
  collaborators: [principal…]
  viewers: [principal…]      # 可选
}
```

`subscribe` 校验：调用方 principal ∈ ACL，否则 403。  
扇出：仅向 **已 subscribe 且仍在 ACL** 的连接推送。

### 8.5 与 discovery_group 的关系

| 机制 | 用途 |
|------|------|
| `discovery_group` | **仅** Placement / peers（能否在某 Node 建成员实例） |
| WorkGroup ACL | **仅** 谁能看组、说话、HITL、管理组 |

创建成员时：**两道闸**——（1）调用方有 `manage_members`；（2）目标 Node 对创建者满足 Placement/peers 规则。

### 8.6 Node 工具执行授权

- Manage 发 `tool.execute` 必须带 `agent_id` + `assign_id`（或等价租约）  
- Node 校验：该 `agent_id` 的 `home_node_id` 是自己，且租约未过期  
- 防止任意 Node 执行别人的成员工具  

### 8.7 云 LLM 清单权限

- `platform_admin` 维护档案  
- 建组时绑定 `llm_profile_id`；仅 **有权用该档案** 的 owner 可建（档案可挂 `allowed_principals` / 团队，v1 可先「admin 可见全部、owner 可用已发布档案」）

### 8.8 迁移现网 token 模型

| 旧 | 新 |
|----|-----|
| `role=admin` | `platform_admin` |
| `role=node` + agent_id | `principal=node` |
| `role=member` + discovery_groups | 拆成：Console 只读员 **或** 映射为若干工作组 ACL，**不再**用 discovery_group 当协作权限 |
| 开放模式（无 token） | 仅开发；生产强制 token |

---

## 9. 明确不做

- 产品内 Docker/remote **沙箱**主路径  
- 已有 Agent 入组、本地 Agent 当 Leader  
- Leader 执行 Node 侧 Shell/FS  
- 无 ACL 的开放订阅  
- 无工作组的跨 Agent A2A  

---

## 10. 分期

| Phase | 内容 |
|-------|------|
| D0 | 本文收口（去沙箱、Supervisor、总列表、权限） |
| D1 | 权限主体 + WorkGroup ACL + 建组/隐式 Leader + 云 LLM 清单 |
| D2 | WS + 订阅（ACL 校验）+ UI 分栏只读 |
| D3 | Human / HITL + 总列表身份字段 |
| D4 | 新建成员（真实环境 home）+ Supervisor `@` + 成员消息拼接 |
| D5 | 下线沙箱 UI/API；拆旧 A2A；token 模型迁移 |

---

## 11. 开工前仅剩

1. **解散时成员实例**：归档 vs 删除（建议归档）  
2. **operator_id v1**：正式账号体系，还是先用 `(node_id, human_name)` 占位并写入 ACL？  
3. **是否要 wg_viewer**，还是 v1 只有 owner + collaborator？  

---

## 12. 一句话

工作组是 Manage 上的 Supervisor 房间：云 Leader 只编排，总消息列表带身份；成员在真实 Node 上执行；隔离靠独立机器而不是内置沙箱；谁能订组/说话/HITL 走统一 ACL，与 Placement 的 discovery_group 分开。
