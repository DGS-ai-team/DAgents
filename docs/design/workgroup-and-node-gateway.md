# 工作组协作 + Node↔Manage 长连接（设计）

> **分支**：`cursor/remote-agent-placement-7e3e`  
> **状态**：**产品方向冻结；文档未闭环**——第二轮 GPT 审核 **Verdict B**：须先改文档再开 D0.5（见 §16）；**禁止**并行实现 Manage turn kernel / Node Worker / WS 主路径  
> **推荐方向**：Manage 云 Leader（Supervisor）+ Timeline/RunHistory；Node 真实环境工具 Worker；权限按 `node_id`；**无远程 Agent / Placement**

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
| ACL 外纯执行 Node | v1 不做；home 必须在 ACL 内 |
| 归档数据保留多久 | 实现期配置，默认不自动物理删 |

---

## 15. GPT 架构审核纪要（2026-07-30）

外部评审结论：**可做，但须先补契约（D0.5），不可直接全面开工。** 风险：**高**（非法 LLM 序列、跨 Node 执行授权、掉线重复副作用）。

### 15.1 致命（开工前必须写入契约）

1. **总列表 ≠ 原样 LLM history**  
   含 `tool_calls` / `tool_call_id` 的回合不能与「进模型只看 role+name+content」混为一谈。  
   **冻结为双层：**  
   - `WorkGroupTimeline`：公开总列表（订阅/审计/UI）  
   - `ActorRunHistory`：Leader/Member **各自**合法连续工具历史  
   - `ContextProjector`：把 timeline 投影进对方上下文，同时保留本 actor 的 tool 配对  

2. **Manage 必须自备 turn kernel**  
   现网编排在 Go Node；Manage 仅有 LLM 配置不够。须明确：Manage 负责 LLM/投影/assign/HITL 状态；Node 只报 **完整 tool JSON Schema** + 执行真实工具；伪工具（ask_user、remember、skills、temporary-agent 等）禁用或迁 Manage。  

3. **home Node 须显式 opt-in**  
   仅「owner 把 node 加进 ACL」不够（等价可在他机跑 Shell）。须：强绑定 `node_id` 凭据、目标 Node **接受成为执行机/邀请**、本地 policy 最终否决、command 带 member/assign/lease。  

4. **掉线 ≠ 可安全重做**  
   改为 `command_id` 幂等 + 状态 `queued→accepted→running→succeeded|failed|indeterminate`；仅未 accepted 可自动重投；已 accepted 结果未知标 `indeterminate`，禁止自动重做非幂等工具。  

### 15.2 重要补充

- Timeline 条目要有稳定 id/seq/assign_id/turn_id；`name` 只服务模型身份，不是主键  
- 协议身份用稳定 id（如 `member_<短id>`），展示名另存 `display_name_at_send`；防 `human_name` 伪装保留名（leader/date/…）  
- provision：Manage 预发 `member_id+provision_id`，Node 幂等 PUT  
- 归档：Manage 先权威失效，再异步回收离线 Node  
- 全组 v1 **最多一个 active assign**；HITL first-resolve 用 DB CAS  
- 成员 Worker **独立 runtime/表**，封锁全部本地 Agent API（不止禁 messages）  
- Registry 在线 Node 列表也要授权可见，不能默认可枚举全网  

### 15.3 开工顺序（取代原 §12 的直开）

1. **D0.5** 冻结契约：schema、状态机、Timeline/RunHistory/Projector、WS 信封、威胁模型、旧数据处置  
2. 停写 Placement/sandbox 新产品入口（先不物理删）  
3. Manage 基座（组存储、ACL、turn kernel）  
4. Node Worker（强身份 WS、幂等 provision、tool schema、command journal）  
5. 纵向闭环：单组单成员单工具 + HITL + 掉线/重启测  
6. 多 Node 订阅与 UI  
7. D5 拆旧 Placement/Edge/A2A/沙箱  

**契约测试门槛（至少）：** tool-call 投影合法、并发 human 排序、掉线不重复副作用、HITL CAS、Manage 重启恢复 pending assign。

---

## 16. 第二轮 GPT 架构审核（2026-07-30）

外部评审（gpt-5.6-sol-xhigh）对全文（含 §14/§15）再审。

**总评：** 产品方向总体自洽，**不必重新冻结方向**。上一轮四个致命项判断正确，但多数仍停在 §15，未回流 §4–§12；部分正文甚至与 §15 **直接冲突**。另发现身份编码、HITL 授权分层、Timeline 可见性、fencing、现网 Node runtime 拆分等开工级缺口。可继续修订文档，**不可据此并行实现**。

**Verdict：B** —— 先改文档，再开 D0.5；不回到产品方向冻结（C）。

### 16.1 已吸收项

| 项 | 位置 | 说明 |
|----|------|------|
| `tool.name` = 工具函数名 | §14.1 | 勿用成员身份覆盖 |
| 成员 LLM 在 Manage；Node 只做工具 | §14.3 | 与冻结方向一致 |
| 成员禁独聊 / 不进本地列表 | §14.4；§15.2 要求独立 runtime | 方向对，实现边界仍弱 |
| Registry + provision | §14.5–§14.6 | 方向对，幂等/opt-in 未闭环 |
| human 入队 / HITL first-resolve / 掉线 | §14.7 | 有覆盖，语义不完整 |
| Timeline / RunHistory / Projector / turn kernel / opt-in / journal | §15 | 正确补丁，多数未进正文 |

### 16.2 上一轮致命项：仍未回流正文

1. **Timeline ≠ LLM history**  
   §4 仍 `Transcript[]`；§6 仍「唯一 Transcript」「进模型只依赖 role+name+content」——与 §15.1 三层模型冲突。须把 `WorkGroupTimeline` + `ActorRunHistory` + `ContextProjector` 提升到 §4，删除「Transcript 原样喂模型」。

2. **Manage turn kernel**  
   §5 只有 Leader 工具列表；无模型调用、合法序列、HITL 暂停、恢复检查点。§12 D1–D4 仍像可在无 kernel 契约下分阶段开工。须先定义 actor run / turn / tool-call / 恢复状态。

3. **home Node 显式 opt-in**  
   §8.3 / §14.5 仍是「加 ACL → 建成员」。ACL = 组聊授权 ≠ 允许他机在本机跑 Shell。须：目标 Node 对 `MemberSpec`/工具范围/工作区的 **ExecutionGrant 接受** + 本地 policy 最终否决。

4. **掉线幂等 / indeterminate**  
   §14.7「掉线即失败」与 §15.1 `indeterminate` 冲突。`accepted` = Node 已持久化 journal；同 `command_id` 可查状态，**禁止**自动重做非幂等；payload hash 不一致拒绝。

§15.2 的稳定 id、幂等 provision、单 active assign、独立 Worker 表、Registry 可见性限制亦未进正文。

### 16.3 新发现致命项

1. **`message.name` 编码不可移植**  
   `member:{display_name}` / 任意 `human_name`（中文、空格、冒号）不具备 provider 可移植性，且可伪装 `leader`/`date`。冻结「身份用 name」可保留，但须改为 provider-safe 稳定名（如 `member_<id>` / `human_<id>`）；展示名另存 `display_name_at_send`。权限/审计依赖服务端盖章的 `actor_id` / `actor_kind` / `authenticated_node_id`，**不依赖 name**。

2. **HITL 混「答问」与「批本机副作用」**  
   collaborator 可答 `ask_user`，**不能**默认批准他机 Shell/FS/Browser。须分：  
   - `information_request`：ACL 内可 first-resolve  
   - `execution_approval`：仅 home Node（或其显式委托）  
   - 其他类型单独定义 authority  

3. **Timeline 可见性未定义**  
   把 Member raw tool args/results 放进总列表会向所有 ACL Node 泄露文件/Shell/凭据。「成员产出进总列表」= 最终产出 + 必要状态事件；原始工具轨迹留在私有 `ActorRunHistory`；审计视图独立、可脱敏。

4. **缺权威状态机与 fencing**  
   ACL 撤销 / 归档 / 重连 / 重复 WS / 延迟 command 并发时，仅「home=self 且租约有效」不够。须 `lease_epoch` / `member_generation` 等 fencing；归档或撤权后旧 command 一律拒绝；已跑副作用走取消/`indeterminate`，不能伪装未发生。

5. **现网 Node runtime 不能直接降级为 `tool.execute`**  
   `turn.Orchestrator` 混 LLM/policy/HITL/skills/hooks/media；`tools.Registry` 混真实工具与伪工具。须独立 `WorkgroupWorker` + 存储，不被 `GET /v1/agents` 枚举。D0.5 明确：何物留 Manage、何物在 Node、background/media/skills 在 v1 禁用或另协议。

6. **HITL resolve 未闭合到合法工具历史**  
   DB CAS 只保证一答案获胜，不保证 actor 恢复正确。须绑定 `actor_run_id` / `turn_id` / `tool_call_id`；获胜答案**恰好一次**写入对应 history 的合法 tool result 再恢复 run。

### 16.4 高风险项（摘要）

- `ContextProjector` 须冻结 actor-relative 规则，非「实测二选一」  
- Member 最终产出 vs Leader assign tool result 去重 / watermark  
- 并行 tool_calls：全配齐或拒绝修复  
- 完整 tool manifest（JSON Schema、revision、side-effect class、policy revision）  
- WS ≠ 事实存储：单调 `seq`、outbox、`client_message_id`、resume、gap-fill  
- 同 `node_id` 重复连接需 `connection_generation` fencing  
- Manage 开放模式 / 共享 token 不得继承到工作组 WS；fail-closed + 独立 Node 凭据  
- 不可变 `MemberSpec` 快照 digest；Node 接受的是快照，不是笼统「某组」  
- 末位 owner、撤 home ACL、归档忙碌成员、LLM 档案删除等边界规则  
- 持久订阅偏好 ≠ 当前 WS；ACL 撤销须立即停增量与历史读  

### 16.5 D0.5 增补清单（开工门槛）

§15.3 一句式清单不够。D0.5 至少冻结：

**A. Schema：** `WorkGroup` / `WorkGroupACL` / `NodeExecutionGrant` / `MemberSpec` / `WorkerBinding` / `ProvisionCommand` / `TimelineEvent` / `ActorRun` / `RunMessage` / `Assign` / `ToolCommand` / `ToolResult` / `HITLRequest` / `HITLResolution` / `Subscription` / `ResumeCursor` / `WSEnvelope` / `CommandAck` / `ArchiveTombstone`；统一定义各 id、`seq`、`lease_epoch`、`connection_generation`、`payload_hash`、`tool_catalog_revision` 等。

**B. 状态机：** WorkGroup `active→archiving→archived`；Grant `invited→accepted→revoked`；Member `requested→provisioning→ready→busy→archived|error`；Assign / ToolCommand / HITL 全状态（含 `indeterminate`）；v1 每组最多一个 active assign；`seq` 在 Manage DB 事务内分配；fencing 顺序。

**C. Turn kernel + 投影：** LLM profile 快照；Manage-native vs Node-executable 白名单；配对/并行/循环上限；watermark、token budget；provider 固定投影；崩溃恢复。

**D. 工具与恢复：** 完整 manifest；`accepted` 仅 journal 持久化后；重复 `command_id` 返回既有状态；hash 冲突拒绝；错误类型表；Manage outbox + Node journal；丢失 → `indeterminate`；归档 tombstone 对账。

**E. 安全：** 每 Node 独立凭据；Registry 授权过滤；Grant 绑 MemberSpec digest；本地 policy 最终否决；reserved name；information HITL vs execution approval 矩阵；Timeline / RunHistory / 审计可见性分层。

**F. 契约测试（在 §15 基础上加）：** golden projection（OpenAI/DeepSeek）；伪装/同名；provision 冲突；command 各阶段断线注入；非幂等 handler 证明不双执行；archive/revoke fencing；HITL 双 resolve / 错误 Node 批准；Manage/Node 各持久化边界重启；WS replay/gap/双连；catalog drift；成员不可经任何本地 Agent API；Timeline 默认不泄 raw tool；旧 Placement/A2A 不能驱动成员。

### 16.6 方向冻结一致性

| 冻结表述 | 是否矛盾 | 前提 |
|----------|----------|------|
| 成员不进本地列表 × 工具在 home 执行 | 否 | 独立 WorkerBinding，非普通 Agent + UI 过滤 |
| Leader 仅编排 × 成员 LLM 在 Manage | 否 | Leader=Manage-native；Member 工具=Node-executable |
| `message.name` × 工具函数名 | 否 | 按 role 分域；协议身份另有稳定字段 |
| 无产品沙箱 | 否 | 独立机器 + ExecutionGrant + 本地 policy |

**须限定的冻结表述：**「ACL 内可参与 HITL」≠「可批准他机执行副作用」。

**正文冲突（改文档优先）：** §6 单 Transcript vs §15 双层；§8 ACL 落执行体 vs opt-in；§14.7 掉线即失败 vs `indeterminate`；display name 入 `name` vs 稳定协议身份；§14.4「隐藏」vs 独立表封锁 API；§12 分期 vs §15.3 顺序。

### 16.7 建议改文档章节（D0.5 前）

1. **§0**：规范优先级；把 D0.5、opt-in、双层历史、`indeterminate`、HITL 分层写入决策表  
2. **§4**：`Timeline + ActorRunHistory + ContextProjector + WorkerBinding` 替换单 Transcript  
3. **§6**：改名「Timeline、运行历史与身份投影」；稳定 provider-safe `name`  
4. **§7**：补 WorkGroup / Member / Assign / HITL / 归档状态机  
5. **§8**：分开 ACL、ExecutionGrant、本地 policy、approval authority、fencing  
6. **§9–§10**：持久订阅、cursor、WS 信封、outbox/inbox、重复连接  
7. **§12**：以 §15.3 重写；D0.5 前禁止并行实现 runtime 主路径  
8. **§14**：已确认补丁回写正文；删「二选一」「WS 或 HTTP」等未冻结表达  
9. **§15**：保留历史审核；**不再**承担规范定义（本轮后以 §16 + 修订正文为准）  
10. **附录**：JSON Schema、状态转换表、权限矩阵、故障恢复矩阵、跨 Go/Python golden fixtures  

### 16.8 最短可行纵向闭环

**应包含：** 两 Node（A owner、B home）；A 建组 + B ACL + B 对 MemberSpec **显式 Grant**；幂等 provision（不出现在本地 Agents API）；human → Leader 同步 assign；Member 在 Manage 跑 loop + B 上单一同步工具（建议首工具 `read_file`）；产出进 Timeline + Leader tool result 合法配对；一次 information HITL 双端 CAS；WS/Manage/Node 在 accepted 前后重启测 `indeterminate`；归档 fencing；最小 UI（订阅/Timeline/发言/HITL），ACL 配置可先走 API/Console。

**刻意不包含：** 多成员/并行 assign；background/流式工具/Browser/媒体/远程桌面；skills/remember/child/A2A；已有 Agent 入组；按人 ACL；raw tool 广播；Placement 物理删除；自动重做未知副作用。
