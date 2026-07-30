# 工作组协作 + Node↔Manage 长连接（设计）

> **分支**：`cursor/remote-agent-placement-7e3e`  
> **状态**：**主体冻结**（可开工实现）  
> **推荐方向**：Manage 云 Leader（Supervisor）+ 总消息列表；Node 真实环境执行；权限按 `node_id`；**无远程 Agent / Placement**

---

## 0. 已拍板（最终）

| 项 | 决策 |
|----|------|
| 跨 Agent | **必须**经工作组 |
| Leader | 隐式云 Agent；Manage 云 LLM；**仅编排工具**（Supervisor） |
| `@` | 同步交接；成员产出写入总消息列表；身份用消息 **`name`**（同 date/human） |
| 成员 | **只新建**；一成员一工作组；工具在 **home Node 真实环境** |
| 沙箱 | **产品废弃**；隔离 = 单独机器部署 Node |
| 解散 | **归档**组会话 / Leader / 成员实例（可查不可当活跃用） |
| 权限主体 | **以 `node_id` 为单位**（ACL 授给 Node，不授给 operator 账号） |
| 远程 | **移除远程 Agent / Placement**；远程协作 **只通过工作组** |
| UI | `/ui`：本地 Agents ‖ 已订阅工作组 分栏 |
| 订阅 | 须在工作组 ACL 内（按 node_id） |

---

## 1. 一句话

工作组是 Manage 上的 Supervisor 房间：云 Leader 编排；成员在指定 Node 真实环境执行；总消息列表带身份；多 Node 按 ACL 订阅组聊/HITL；**不再有「远端放置的 Agent 引用」**——跨机器只表现为「成员 home 在别的 Node」+「别的 Node 订阅同一组」。

---

## 2. 移除：远程 Agent / Placement

### 2.1 废弃范围

| 旧能力 | 处置 |
|--------|------|
| `origin=remote` owner 引用 / 双主人 | **删除**（产品与主路径） |
| Control 远端创建/双删、`/v1/peers/nodes` 放置 | **删除**或冻结后删 |
| Edge Tunnel 代理远端 Agent 聊天/SSE | **删除**（组聊走 Manage 总列表 + WS） |
| 屏幕旁观挂在「远端 Agent」 | **不做**（若要桌面能力，另开需求，不绑 Placement） |
| UI「运行位置选 peer」 | **删除** |
| `placement.allow_peer_create` 等 | **删除**或仅内部无产品入口 |

### 2.2 跨机器如何表达（新）

```text
工作组成员 M 的 home_node_id = node-B
→ M 的工具在 node-B 真实执行
→ 不是「在 A 上放了一个 remote stub 代理 B」

Node-A / Node-C 若在 ACL 中
→ 可订阅该工作组，看总列表、HITL、发 human 消息
→ 不是「拥有远端 Agent」
```

本地独聊 Agent：**始终** `origin=local`，只活在本 Node。

### 2.3 与本分支已写 Placement 代码的关系

本分支上的 Placement/Edge/屏幕旁观实现视为 **实验路径，按本设计回退/拆除**（实现 Phase 与工作组并行规划：先停产品入口，再删代码）。`docs/design/remote-agent-placement.md` 标记为 **superseded**。

---

## 3. 执行环境（无沙箱）

- 成员 / 本地 Agent：真实 FS、Shell、Browser  
- 要隔离：部署独立 Node 机器，把成员 `home_node_id` 指过去  
- 下线产品内 Docker / remote sandbox 主路径  

---

## 4. 概念模型

```text
Manage
  ├── 云 LLM 清单
  ├── 权限：WorkGroup ACL（principal = node_id）
  └── WorkGroup
        ├── Leader（云 Supervisor）
        ├── Transcript[]（总消息列表）
        ├── Members[]（新建，home_node_id，真实执行）
        └── 订阅：已 subscribe 且仍在 ACL 的 node_id

Node
  ├── 本地 Agents（独聊）
  ├── 工作组（本 node_id 在 ACL 且已订阅）— UI 分栏
  └── 工具 Worker（仅执行 home=本机 的成员工具）
```

---

## 5. Leader = Supervisor

仅 Manage 内编排工具，例如：

- `assign_workgroup_task`（`@`，同步）  
- `list_workgroup_members`  
- （可选）`browse_member_status` / `cancel_assign`  

不执行 Shell/FS/Browser。

---

## 6. 总消息列表与身份（`name` 字段）

唯一 Transcript。身份标识 **对齐现网 LLM message 的 `name` 字段**（与当天日期 Hook 同源），**不要**再搞一套平行的 `actor` 信封给模型看。

### 6.1 现网锚点

- `node/internal/llm/message_names.go`：`user` 消息用 `name` 区分来源（`human` / `trigger` / `date` / `a2a_inbox` …）  
- `InjectTodayDateHook`：插入 `role=user`、`name=date`、content=`当天日期为：YYYYMMDD`  
- API：`user_message_name` → 入队时写入 message.`name`（空则 `human`）  
- 出站：`MessageToAPIPayload` 把 `name` 传给 Chat Completions  

工作组沿用同一机制：多说话人 = 多条消息上的不同 `name`。

### 6.2 约定（组会话）

| 来源 | role | name | 说明 |
|------|------|------|------|
| 订阅端人工 | `user` | **`human_name`**（空则 `human`） | 同 Node 多人靠不同 name |
| 日期等系统注入 | `user` | `date` 等既有常量 | 不变 |
| Leader 输出 | `assistant` | `leader` | UI/审计用；喂模型时注意 §14.2 |
| Member 的 assistant | `assistant` | `member:{display_name}` | 拼入总列表；喂模型时注意 §14.2 |
| Member 的 tool 结果 | `tool` | **工具函数名**（不变） | 身份靠 `assign_id` / 前后文，勿覆盖 name |
| `@` 边界 | `user` | `workgroup_assign` | 可选 |

持久化可另存 `home_node_id` / `agent_id` 供 UI/审计；**进模型的上下文只依赖 `role` + `name` + `content`**（与 date 一致）。

### 6.3 UI

时间线按 `name` 着色/徽章（`date` 弱化、`leader` / `member:*` / 人名）。  
`@` 附带近 ≤10 条、`browse_leader_context` 均读同一总列表。

---

## 7. 生命周期

1. **建组**：Leader + 空列表；创建者 `node_id` → `wg_owner` + 自动订阅  
2. **新建成员**：指定 `home_node_id`（该 Node 须在线且创建者有权；见 §8）；禁止拉已有 Agent  
3. **订阅**：仅 ACL 内 `node_id`  
4. **解散**：**归档**——组、Leader、Transcript、成员实例均归档（只读历史，不可再 `@` / 不可当活跃本地 Agent）  

---

## 8. 权限（以 `node_id` 为单位）

### 8.1 主体

| Principal | 说明 |
|-----------|------|
| `platform_admin` | Manage 全局（Console / LLM 清单） |
| `node_id` | **唯一协作授权单位**；WS 与组 ACL 均按 Node |

不引入独立 `operator_id` 账号体系（v1）。同一 Node 上多人用不同 `human_name` 发言，但 **权限相同**（都来自该 `node_id` 的角色）。

### 8.2 工作组角色

| 角色 | 权限 |
|------|------|
| **wg_owner** | 改 ACL、建/归档成员、解散（归档）、订阅、发言、HITL |
| **wg_collaborator** | 订阅、发言、HITL；不能改 ACL / 解散 |

```text
WorkGroupACL {
  owners: [node_id, …]
  collaborators: [node_id, …]
}
```

- 建组 Node ∈ owners  
- `subscribe`：请求 Node ∈ owners ∪ collaborators，否则 403  
- **不用** `discovery_group` 做订阅授权  

### 8.3 创建成员时的 Node 约束

创建成员到 `home_node_id=H`：

1. 调用方 Node 对组有 `manage_members`（owner）  
2. **H 在线**且已与 Manage 建立 WS  
3. v1 建议：**H 必须已在该组 ACL 中**（owner 或 collaborator）——避免往「从未授权进组」的机器上落执行体；owner 可先把 H 加进 ACL 再建成员  

（若产品希望「ACL 外机器只当纯执行机、不能看组聊」，可二期拆 `executor_nodes`；v1 不拆，保持简单。）

### 8.4 工具执行

Manage → `tool.execute` 到 home Node；该 Node 校验 `agent_id.home == self` 且租约有效。

### 8.5 废弃 discovery_group 的协作含义

| 机制 | 用途（新） |
|------|------------|
| WorkGroup ACL（node_id） | 订阅 / 发言 / HITL / 管组 /（v1）成员落点 |
| `discovery_group` | **可整段降级或仅 Console 标签**；不再支撑 Placement peers；实现期可先保留字段但产品不依赖 |

---

## 9. UI

```text
左侧
  本地 Agents
  工作组（本 node 已订阅）
```

组聊：总列表时间线、HITL、`human_name` 输入；无「远程 Agent」入口。

---

## 10. 长连接

每 Node 一条 WS：announce/ping、组扇出、tool RPC、human/HITL。  
废除 A2A inbox/task 轮询主路径。

---

## 11. 明确不做

- 远程 Agent / Placement / Edge 代理聊天 / 远程桌面旁观（本设计）  
- 产品内沙箱  
- 已有 Agent 入组、本地升 Leader  
- Leader 跑 Node 工具  
- 按人账号的细粒度 ACL（v1）  
- 开放订阅  

---

## 12. 分期

| Phase | 内容 |
|-------|------|
| D0 | 本文冻结 |
| D1 | ACL（node_id）+ 建组/Leader + 云 LLM + 归档模型 |
| D2 | WS + 订阅 + UI 分栏 |
| D3 | Human/HITL + 总列表身份 |
| D4 | 新建成员 + Supervisor `@` + 消息拼接 |
| D5 | **拆除** Placement/Edge/remote stub/沙箱产品路径；拆旧 A2A |

---

## 13. 相关文档状态

| 文档 | 状态 |
|------|------|
| 本文 | **现行** |
| `remote-agent-placement.md` | **superseded**（远程改走工作组） |
| `node-centric-architecture-cleanup.md` | 继续：去 ops/compliance、去沙箱、拆旧 A2A |
---

## 14. 方案审查补丁（2026-07-30）

自检后需 **补充/纠正** 如下（纳入冻结范围）。

### 14.1 纠正：`name` 与 tool 消息冲突

现网 **`role=tool` 的 `name` = 工具函数名**（给 API / UI），不能改成 `member:xxx`。

| 消息 | `name` 用法 |
|------|-------------|
| `user` | 说话人身份：`human_name` / `date` / `workgroup_assign` … |
| `assistant` | 说话人身份：`leader` 或 `member:{display_name}`（见下） |
| `tool` | **保持工具名**；归属靠前后文 / `assign_id`（UI 侧栏展示成员） |

### 14.2 纠正：部分模型忽略 `assistant.name`

DeepSeek/OpenAI 对 **`user.name`** 支持明确；**`assistant.name` 可能被忽略**。

- 写入总列表时仍带 `name`（UI/审计用）  
- **拼给另一个模型看时**：若实测丢 `assistant.name`，对该条做投影——例如改为 `user` + 同名 `name`，或 content 前缀 `[member:代码员]`（实现期二选一，优先保 `name` 投影）  
- **`user`/`date` 路径不变**

### 14.3 补充：成员回合的 LLM 也在 Manage

| 回合 | LLM | 工具 |
|------|-----|------|
| Leader | Manage 云 LLM | 仅编排工具（Manage 内） |
| Member（被 `@`） | **同样在 Manage** 跑 loop | `tool.execute` → **home Node** |

成员 **不是** 在 home Node 上开独聊 turn；Node 只做工具 Worker。成员可用组级云 LLM，或建成员时指定 Manage 清单中的档案（v1 可先共用组绑定档案）。

### 14.4 补充：工作组成员不出现在「本地 Agents」列表

- 成员实例在 home Node 上有运行时/工作区，但 **UI 本地 Agents 分栏不展示**（或标记为系统/不可点）  
- **禁止**对成员做独聊 `POST /v1/messages`（返回明确错误）  
- 只通过工作组 `@` 驱动，避免双脑  

### 14.5 补充：无 Placement 后如何「看见」其他 Node

废除 peers 放置后，建 ACL / 选 `home_node_id` 依赖：

- Manage **Registry 在线 Node 目录**（WS announce 维持 online）  
- Console / 建组 UI：`GET` 在线 `node_id` 列表 → 加入 ACL → 再在其上新建成员  

不靠 `discovery_group` 交集。

### 14.6 补充：Manage → Node 建成员协议

替代旧 Control Placement，WS（或受控 HTTP）例如：

`workgroup.member.provision` → home Node 创建绑定工作组的运行时（模板/工具组/工作区）→ 返回 `agent_id`  
`workgroup.member.archive` → 解散时归档  

### 14.7 补充：并发与 HITL

- Leader 同步 `@` 进行中：新 human 消息 **入队**，当前交接结束后再喂 Leader（不打断 `@`）  
- HITL：多订阅 Node 可见；**先 resolve 者生效**，其余得「已处理」  
- home Node 掉线：进行中的 tool/assign **失败回写总列表**，Leader 可再计划  

### 14.8 补充：`accept_inbound` / 旧 A2A

- Node 级 `agent_invoke` / inbox **废除**  
- 机器只要连上 Manage WS 且被 ACL/建成员引用即可；不必再保留「A2A 入站」产品开关（实现期可删或忽略）  
- 本地 **child/临时 Agent** 仍为 Node 内能力，与工作组无关  

### 14.9 仍可实现期再定（非阻塞冻结）

| 项 | 建议默认 |
|----|----------|
| 成员 Manage LLM 档案 | v1 与组合用同一云档案 |
| `assistant.name` 投影策略 | 先写入；测提供方后再定是否改 user 投影 |
| ACL 外纯执行 Node | v1 不做；home 必须在 ACL 内 |
| 归档数据保留多久 | 实现期配置，默认不自动物理删 |
