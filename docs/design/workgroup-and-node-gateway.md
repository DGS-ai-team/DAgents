# 工作组协作 + Node↔Manage 长连接（设计）

> **分支**：`cursor/remote-agent-placement-7e3e`  
> **状态**：**产品方向冻结；D0.5 契约已冻结** — 见 [`workgroup-d05-contracts.md`](./workgroup-d05-contracts.md)（§19 Verdict A）；**D0.9 / D1 / D2 已完成**；**D3 纵向闭环进行中**（Manage `vertical` + Node `read_file` / journal 恢复）  
> **规范优先级**：§0–§13（产品正文）> [`workgroup-d05-contracts.md`](./workgroup-d05-contracts.md)（已冻结）> §15/§16（历史审核）  
> **推荐方向**：Manage 云 Leader（Supervisor）+ Timeline/RunHistory；成员资产绑组存 Manage；Node 仅工具调用；权限按 `node_id`；**无远程 Agent / Placement**

---

## 0. 已拍板（最终）

| 项 | 决策 |
|----|------|
| 跨 Agent | **必须**经工作组 |
| Leader | 隐式云 Agent；Manage 云 LLM；**仅编排工具**（Supervisor） |
| `@` | 同步交接；成员**最终产出**写入 Timeline；身份用 provider-safe **`message.name`** |
| 成员 | **只新建**；一成员一工作组；**生命周期随工作组归档而结束**（Manage 权威） |
| 成员资产绑定 | 侧车 / 记忆 / 工具权限 / LLM 档案 / RunHistory **全部存 Manage，绑定工作组（及成员）**；Node **只做工具调用**（真实 FS/Shell/Browser）+ 本地否决 |
| 成员资产禁令 | 禁止在 Node 落权威侧车副本当「Agent 配置」；禁止继承 home 独聊侧车/全局 memory/默认工具组 |
| 沙箱 | **产品废弃**；隔离 = 单独机器部署 Node |
| 解散 | **归档**（可查不可当活跃用） |
| 权限主体 | **以 `node_id` 为单位**（不授给 operator 账号） |
| 执行授权 | ACL ≠ 执行权；home 须对 `MemberSpec` **显式 ExecutionGrant**；本地 policy 最终否决 |
| HITL | **分层**：信息型 ACL 内可 resolve；执行批准仅 home（或其显式委托） |
| 历史模型 | **双层**：`WorkGroupTimeline`（公开）+ `ActorRunHistory`（私有合法工具史）+ `ContextProjector` |
| 掉线 | `command_id` 幂等；`accepted` 后未知 → `indeterminate`；禁止自动重做非幂等 |
| 远程 | **移除远程 Agent / Placement**；跨机器只走工作组 |
| UI | `/ui`：本地 Agents ‖ 已订阅工作组 分栏 |
| 订阅 | 须在工作组 ACL 内（按 node_id） |
| 开工门槛 | **D0.5 契约**（[`workgroup-d05-contracts.md`](./workgroup-d05-contracts.md)）退出检查表通过后，再 Manage 基座 → Node Worker → 纵向闭环 |

---

## 1. 一句话

工作组是 Manage 上的 Supervisor 房间：云 Leader 编排；**成员配置（侧车/权限/记忆）绑组存在 Manage**；home Node 只做真实工具调用；公开 Timeline + 私有 RunHistory；多 Node 按 ACL 订阅；**无远端放置的 Agent 引用**。

---

## 2. 移除：远程 Agent / Placement

### 2.1 废弃范围

| 旧能力 | 处置 |
|--------|------|
| `origin=remote` owner 引用 / 双主人 | **删除**（产品与主路径） |
| Control 远端 create/双删、`/v1/peers/nodes` 放置 | **删除**或冻结后删 |
| Edge Tunnel 代理远端 Agent 聊天/SSE | **删除**（组聊走 Manage Timeline + WS） |
| 屏幕旁观挂在「远端 Agent」 | **不做** |
| UI「运行位置选 peer」 | **删除** |
| `placement.allow_peer_create` 等 | **删除**或仅内部无产品入口 |

### 2.2 跨机器如何表达（新）

```text
工作组成员 M 的 home_node_id = node-B
→ M 的工具在 node-B 真实执行（WorkerBinding）
→ 不是「在 A 上放了一个 remote stub 代理 B」

Node-A / Node-C 若在 ACL 中且已订阅
→ 可看 Timeline、参与信息型 HITL、发 human 消息
→ 不是「拥有远端 Agent」
```

本地独聊 Agent：**始终** `origin=local`，只活在本 Node。

### 2.3 与本分支已写 Placement 代码的关系

本分支 Placement/Edge/屏幕旁观实现视为 **实验路径，按本设计回退/拆除**（先停产品入口，再删代码）。`docs/design/remote-agent-placement.md` 标记为 **superseded**。

---

## 3. 执行环境（无沙箱）

- 成员工具 / 本地 Agent：真实 FS、Shell、Browser  
- 要隔离：部署独立 Node，把成员 `home_node_id` 指过去  
- 下线产品内 Docker / remote sandbox 主路径  
- **无产品沙箱 ≠ 无安全边界**：ExecutionGrant + 本地 policy + fencing 是边界  

---

## 4. 概念模型

```text
Manage
  ├── 云 LLM 清单（组/成员绑定档案）
  ├── Turn kernel（Leader/Member loop、投影、HITL、恢复）
  ├── 权限：WorkGroupACL + NodeExecutionGrant
  └── WorkGroup
        ├── Leader（云 Supervisor）
        ├── WorkGroupTimeline[]     ← 公开总列表（UI/订阅/审计摘要）
        ├── ActorRunHistory[]      ← Leader/Member 各自合法工具历史（私有）
        ├── ContextProjector       ← Timeline → 对方上下文；保留本 actor tool 配对
        ├── Members[] + MemberSpec ← 不可变快照 + digest
        └── Subscriptions[]        ← ACL 内且已订阅的 node_id

Node
  ├── 本地 Agents（独聊；与工作组隔离）
  ├── 工作组 UI（本 node 在 ACL 且已订阅）— 分栏
  └── WorkgroupWorker
        ├── WorkerBinding（独立表；不进 GET /v1/agents）
        ├── ExecutionGrant（接受的 MemberSpec digest）
        ├── 工作区 + command journal
        └── 仅执行 home=本机 的真实工具
```

**禁止**：把 `WorkGroupTimeline` 原样当作任一 Actor 的 LLM history。

---

## 5. Leader / Member 与 Manage turn kernel

### 5.1 Leader = Supervisor

仅 Manage 内编排工具，例如：

- `assign_workgroup_task`（`@`，同步）  
- `list_workgroup_members`  
- （可选）`browse_member_status` / `cancel_assign`  

不执行 Shell/FS/Browser。

### 5.2 Member 回合

| 回合 | LLM | 工具 |
|------|-----|------|
| Leader | Manage 云 LLM | 仅 Manage-native 编排工具 |
| Member（被 `@`） | **同样在 Manage** 跑 loop | Node-executable → `tool.execute` → home |

成员 **不是** 在 home Node 开独聊 turn。

### 5.3 Turn kernel（Manage 必须自备）

现网编排在 Go Node；Manage 仅有 LLM 配置不够。Manage 负责：

- LLM 调用、合法消息序列、tool_call 配对  
- ContextProjector、assign 状态、HITL 暂停/恢复  
- ActorRunHistory 写入与崩溃恢复检查点  

Node 只：宣告 **完整 tool JSON Schema** + 执行真实工具。  
伪工具（`ask_user` / `remember` / skills / child-agent 等）：**v1 禁用或迁 Manage**，不得经 `tool.execute` 远程跑。

v1：**全组最多一个 active assign**；Leader run 单写者。

---

## 6. Timeline、运行历史与身份投影

### 6.1 双层 + 投影

| 层 | 用途 | 可见性 |
|----|------|--------|
| `WorkGroupTimeline` | 组聊 UI、订阅扇出、审计摘要、成员**最终产出**与状态事件 | ACL 内已订阅 Node |
| `ActorRunHistory` | 该 Actor 连续合法 assistant/tool 历史（含 `tool_calls` / `tool_call_id`） | 仅 Manage turn kernel（+受控审计） |
| `ContextProjector` | 把 Timeline 快照投影进对方上下文，同时保留本 Actor 的 tool 配对 | 内部 |

**「成员产出进总列表」** = 最终回复 + 必要状态（assign 开始/结束、`indeterminate` 等）。  
**原始 tool args/results 默认不进 Timeline**（防凭据/文件/Shell 广播）；需审计时走独立可脱敏视图。

Member 最终产出同时可能成为 Leader 的 `assign_workgroup_task` tool result：须 **watermark / 去重**，避免 Leader 看两遍。

### 6.2 现网锚点（身份机制）

沿用 LLM message 的 `name` 字段（与 `InjectTodayDateHook` / `message_names.go` 同源），**不要**另造平行 actor 信封给模型看。但：

- `name` **只服务模型身份与 UI**，不是主键  
- 权限/审计依赖服务端盖章：`actor_id` / `actor_kind` / `authenticated_node_id` / `event_id` / `seq`

### 6.3 Provider-safe `name` 约定

| 来源 | role | name（协议） | 展示 |
|------|------|--------------|------|
| 订阅端人工 | `user` | `human_<stable_id>`（未辨识则 `human`） | `display_name_at_send` |
| 系统注入 | `user` | `date` 等保留常量 | 弱化 |
| Leader | `assistant`（投影时可能改写） | `leader` | Leader |
| Member 产出 | 同上 | `member_<stable_id>` | `display_name_at_send` |
| Member tool（仅 RunHistory） | `tool` | **工具函数名**（不变） | UI 侧栏靠 `assign_id` |
| `@` 边界（可选） | `user` | `workgroup_assign` | — |

规则：

- **禁止**把 display_name / 任意中文/`human_name` 直接写入协议 `name`  
- 保留名：`leader` / `date` / `human` / `workgroup_assign` / … 不可被人工伪装  
- `role=tool` 的 `name` **永远是工具函数名**（对齐现网）

### 6.4 投影规则（冻结，非「实测二选一」）

Actor-relative：

1. **本 Actor** 的 assistant/tool 历史：保持合法配对原样喂模型  
2. **其他 Actor** 的已提交产出：投影为 provider-safe 外部输入（优先保留 `user` + 同名 `name`；必要时 content 前缀仅作兜底，且须进 golden fixtures）  
3. DeepSeek/OpenAI 对 `assistant.name` 可能忽略：投影层统一处理，Timeline 仍存协议 `name` 供 UI/审计  

并行 `tool_calls`：v1 须 **全配齐结果** 或 **拒绝并修复**；不得半配齐。

### 6.5 UI

Timeline 按 `actor_kind` + `display_name_at_send` 着色；`date` 弱化。  
`@` 附带近 ≤10 条 Timeline、browse 均读 Timeline（非 RunHistory）。

---

## 7. 生命周期与状态机

### 7.1 产品步骤

1. **建组**：Leader + 空 Timeline；创建者 `node_id` → `wg_owner` + 自动订阅  
2. **加 ACL / 邀请执行**：目标 home 须 **接受 ExecutionGrant**（绑 MemberSpec digest）后才能 provision  
3. **新建成员**：禁止拉已有本地 Agent；幂等 `provision_id`  
4. **订阅**：仅 ACL 内；持久偏好 ≠ 当前 WS 连接  
5. **解散**：**归档**——Manage 先权威失效，再异步回收离线 Node；只读历史，不可再 `@`

### 7.2 状态机（D0.5 细化表）

| 实体 | 状态 |
|------|------|
| WorkGroup | `active → archiving → archived` |
| ExecutionGrant | `invited → accepted → revoked` |
| Member | `requested → provisioning → ready → busy → archived \| error` |
| Assign | `queued → running → awaiting_hitl → succeeded \| failed \| canceled \| indeterminate` |
| ToolCommand | `queued → accepted → running → succeeded \| failed \| rejected \| canceled \| indeterminate` |
| HITL | `pending → resolved \| expired \| canceled` |

不变量：

- v1 每组最多一个 active assign  
- Timeline `seq` 在 Manage DB 事务内分配  
- fencing：`lease_epoch` / `member_generation` / `connection_generation`；归档或撤权后旧 command 一律拒绝  
- 已产生副作用只能取消/`indeterminate`，不能伪装未发生  

---

## 8. 权限（分层）

### 8.1 主体

| Principal | 说明 |
|-----------|------|
| `platform_admin` | Manage 全局（Console / 云 LLM 清单） |
| `node_id` | **协作与执行授权单位**；WS/ACL/Grant 均按 Node |

不引入 `operator_id`（v1）。同 Node 多人用不同展示名发言，**权限相同**。

### 8.2 WorkGroup ACL（组聊授权）

| 角色 | 权限 |
|------|------|
| **wg_owner** | 改 ACL、发起成员邀请/归档、解散、订阅、发言、**信息型** HITL |
| **wg_collaborator** | 订阅、发言、**信息型** HITL；不能改 ACL / 解散 |

```text
WorkGroupACL { owners: [node_id…]; collaborators: [node_id…] }
```

- `subscribe`：请求 Node ∈ owners ∪ collaborators，否则 403  
- ACL 撤销：**立即**停增量与历史读  
- **不用** `discovery_group` 做订阅授权  

### 8.3 ExecutionGrant（执行授权 ≠ ACL）

创建成员到 `home_node_id=H`：

1. 调用方对组有 `manage_members`（owner）  
2. H 在线且 WS 已认证（独立 Node 凭据；fail-closed，不继承 Manage 开放模式）  
3. v1：H 已在该组 ACL 中  
4. **H 显式接受**绑定 `MemberSpec` digest 的 ExecutionGrant（工具范围、工作区、policy 上限）  
5. H 本地 policy 对每条 command **最终否决权**  
6. command 必须带 `member_id` / `assign_id` / `lease_id` / `lease_epoch` / `payload_hash`

仅「owner 把 H 加进 ACL」**不够**落执行体。

### 8.4 HITL 授权矩阵

| 类型 | 谁可 resolve |
|------|----------------|
| `information_request`（如问事实） | ACL 内授权订阅者；DB CAS first-resolve |
| `execution_approval`（批 Shell/FS/Browser 等） | **仅 home Node**（或其显式委托） |
| 其他 | 单独定义 |

HITL 必须绑定 `actor_run_id` / `turn_id` / `tool_call_id`；获胜答案**恰好一次**写入对应 ActorRunHistory 的合法 tool result，再恢复 run。

### 8.5 工具执行与掉线

- Manage → `tool.execute` → home Worker  
- Worker 校验：binding.home == self、Grant 有效、digest 匹配、lease/fencing 有效  
- `accepted` **仅在** command journal 持久化后返回  
- 同 `command_id`：返回既有状态，不重复执行；payload hash 不一致 → 冲突拒绝  
- 仅 `queued`（未 accepted）可自动重投；已 accepted 结果未知 → `indeterminate`  

### 8.6 discovery_group

| 机制 | 用途（新） |
|------|------------|
| WorkGroup ACL | 订阅 / 发言 / 信息型 HITL / 管组 |
| ExecutionGrant | 本机执行成员工具 |
| `discovery_group` | 可降级为 Console 标签；产品不依赖 |

### 8.7 Registry 可见性

在线 Node 目录 **须授权过滤**，不能默认可枚举全网。

---

## 9. 成员资产：侧车、记忆、工具权限（MemberSpec）

> 回答：「新建的工作组成员，侧车文件、记忆文件从哪里取？工具权限呢？」  
> 现网锚点：独聊 Agent 创建见 `handleCreateAgent`；侧车 `EnsureAgentPromptContext`；policy `EnsureAgentPolicy`；工具组 `EnabledToolGroups`。

### 9.0 原则（冻结）：资产随工作组，调用经 Node

**对。** 成员生命周期在 Manage 内随工作组结束而结束，则配置态必须同命运：

```text
Manage（权威，绑定 WorkGroup / Member）
  ├── MemberSpec：侧车正文、工具白名单、policy 上限、LLM 档案…
  ├── 记忆 / RunHistory / Timeline 片段
  └── 归档组时一并归档；无「组没了 Agent 还在 Node 上独活」

Node（无权威人格；仅 Worker）
  ├── 接受 ExecutionGrant（digest）→ 临时能力租约
  ├── tool.execute：真实环境副作用
  └── 本地 policy 可否决；不持有可独立续命的侧车/记忆副本
```

| 存 Manage（绑组） | 经 Node（仅调用） | 不存成「成员配置」 |
|------------------|-------------------|-------------------|
| Soul/User/Custom、工具权限、LLM、记忆、对话史 | `read_file` / `bash` / browser 等真实执行 | Node 上再留一份权威 `soul.md` / 独聊 Agent 行 |

细辨（不推翻原则）：

1. **权限在 Manage，能力在 Node**：白名单说「允许 bash」≠ 该机一定有 bash；实际 = Spec ∩ 本机工具目录 ∩ 本地 policy。  
2. **工作区文件副作用**（工具写出的代码/日志）落在 home 磁盘——那是执行产物，不是侧车配置；归档时可按策略清理工作区，但**不**把它们当成成员人格的权威存储。  
3. **本地否决**是防失控的安全阀，不是第二套权限源；收紧可以，放宽不行。  
4. **组归档后**：Manage 归档 Spec/记忆；Node 回收 WorkerBinding / journal /（可选）工作区；成员不可再被 `@`，也不可变成本地独聊 Agent。

### 9.1 现网独聊路径（为何不能直接复用）

| 资产 | 现网来源 | 问题 |
|------|----------|------|
| 侧车 Soul/User/Custom | `agents.db.agent_prompt_context`；缺失时从 home Node `.runtime/prompt_context/*.md` **迁移拷贝** | 继承本机私有人格/上下文，跨 Node 不确定 |
| 旧 long_term.md | 同上迁移进 `long_term_md` | 可能泄露本机记忆 |
| 结构化长期记忆 | `longterm_store`，scope=`agent`/`global` | `global` 会串本机其他 Agent |
| 工具组 | 模板 `defaults.tools.enabled_groups`；**空数组回退 Node 默认** | 非 fail-closed，可能扩大权限 |
| 审批 policy | `packaging/runtime/policy` 种子，或 home `.runtime/policy` | 非 MemberSpec 权威 |
| Skills/hooks | Node 共享 `.runtime/skills` + 任意 `.so` | 模板只存名字；内容不受 Manage 约束 |
| LLM | 模板 `llm.active` → 本地 `llm_configs.db` | 与「成员 LLM 在 Manage」冲突 |
| 模板 | `packaging/agent-templates/*.yaml` 只有 defaults/sandbox，**无侧车正文** | 只存 `template_id` 不够 |

实例目录 `agents/<id>/{data,history,memory}` 会创建，但 **remember 不写 memory 目录**；权威在 SQLite。

### 9.2 权威归属（冻结）

全部配置态 **绑定工作组，保存在 Manage**；home Node **只执行工具调用**（外加 Grant 租约与否决）。

| 资产 | 权威位置（Manage，随组归档） | home Node |
|------|------------------------------|-----------|
| Soul / User / Custom 等侧车正文 | `MemberSpec` 正文或不可变 blob | **不落权威副本**；不继承本机 prompt_context |
| 成员长期记忆 / remember | Manage（scope=该 member） | 不写 `longterm_store` |
| 对话 / RunHistory / Timeline | Manage | 无独聊 session |
| 工具可见性白名单 | Spec 展开到**工具名**（fail-closed） | 按租约挂载真实工具并执行 |
| 审批 policy 上限 | Spec | 本地 policy **只可更严** |
| 工作区路径契约 | Spec + Grant | 物化目录；工具副作用在此，非人格存储 |
| Skills / hooks / child / media / background | v1 禁用（若未来启用仍归 Manage 清单） | 不挂共享 `.so` |
| 云 LLM 档案 | Spec（v1 可与组共用） | 不读本地 `llm_configs` 驱动成员 |

### 9.3 MemberSpec 必须包含（不可变快照 + digest）

创建时由 owner 在 Manage 组装；digest 绑定 ExecutionGrant。至少：

```text
MemberSpec {
  member_id, workgroup_id, home_node_id, display_name
  member_generation
  llm_profile_id + revision          # Manage 云清单
  max_tool_loops
  prompt: {
    soul_md, user_md, custom_md      # 显式正文；允许全空
    # 禁止「从 home 模板/旧文件偷」
  }
  memory: {
    remember_enabled: false          # v1 建议关；若开则只写 Manage
    initial_entries: []              # 可选显式种子，非本机迁移
  }
  tools: {
    allow_names: [read_file, …]      # fail-closed；展开到名
    # 或 allow_groups + 显式 deny_names；空 = 无工具
    side_effect_classes: […]
  }
  policy_ceiling: { … }              # 请求的最松审批；Node 可更严
  workspace: { root_contract, … }
  skills: disabled                   # v1
  hooks: disabled                    # v1
  digest
}
```

**不能只存 `template_id`**：各 Node 模板内容可能不同/被覆盖。若 UI 用「工作组模板」预填，必须在 Manage **展开正文与工具名后**再快照。

### 9.4 Provision 时 home Node 物化什么

`workgroup.member.provision`（幂等 `provision_id`）在 home 上：

1. 写入独立 `WorkerBinding`（**不是**普通 `agents` 行）  
2. 接受/校验 ExecutionGrant（digest 一致）  
3. 创建成员工作区目录  
4. 初始化 command journal  
5. 按 `allow_names` ∩ 本机能力 挂载真实工具；上报完整 JSON Schema + `tool_catalog_revision`  
6. 加载本地 policy 引擎（ceiling ∩ 本机）  
7. **不**调用 `EnsureAgentPromptContext` / 不读本机 `soul.md`  
8. **不**写入可被 `GET /v1/agents` 枚举的记录；封锁全部本地 Agent API  

返回：`worker_id`（可与 member_id 相同或映射），不是「可独聊的 agent_id」。

### 9.5 建成员 UX（建议）

创建向导在 **Manage / 组 UI**（非本机「从模板新建 Agent」）：

1. 选 home Node（ACL + 在线 + 可邀请）  
2. 填 display_name、云 LLM 档案  
3. **显式编辑** Soul/Custom（可从「工作组侧车模板库」预填，模板存 Manage）  
4. 勾选工具名（默认最小：如仅 `read_file`）  
5. 发起 Grant 邀请 → home 接受 → provision  

---

## 10. UI 与订阅

```text
左侧
  本地 Agents
  工作组（本 node 已订阅）
```

- 组聊：Timeline、信息型 HITL、展示名输入；无「远程 Agent」入口  
- **持久订阅偏好** 与 **当前 WS 连接** 分离；断线可 resume cursor  
- ACL 撤销后 UI 立即不可读增量/历史  

---

## 11. 长连接（WS）

每 Node 一条（或一代）认证 WS：announce/ping、组扇出、tool RPC、human/HITL、provision。

要求：

- WS **不是**事实存储：Manage durable outbox + Node durable inbox/journal  
- 每组单调 `seq`、`client_message_id` 去重、resume cursor、gap-fill、背压  
- 同 `node_id` 重复连接：`connection_generation` fencing，旧连接失效  
- 废除 A2A inbox/task 轮询主路径  

信封字段（D0.5 定稿）：`WSEnvelope` / `CommandAck` / `ResumeCursor` 等。

---

## 12. 明确不做

- 远程 Agent / Placement / Edge 代理聊天 / 远程桌面旁观  
- 产品内沙箱  
- 已有 Agent 入组、本地升 Leader  
- Leader 跑 Node 工具  
- 按人账号细粒度 ACL（v1）  
- 开放订阅  
- 成员走独聊 `POST /v1/agents` / `POST /v1/messages`  
- v1：skills / remember（若未迁 Manage）/ child-agent / 共享 `.so` hooks  
- Timeline 广播 raw tool args/results  
- 自动重做 `indeterminate` 非幂等工具  

---

## 13. 分期（取代旧 D1–D4 直开）

| Phase | 内容 |
|-------|------|
| **D0** | 产品方向冻结（本文 §0） |
| **D0.5** | 契约：[`workgroup-d05-contracts.md`](./workgroup-d05-contracts.md) + `fixtures/workgroup-d05/` — **已冻结（§19 A）** |
| **D0.9** | 停写 Placement 新产品入口（Node API/UI 返回 `placement_deprecated`）— **已完成** |
| **D1** | Manage 基座：组存储、ACL、Grant、MemberSpec、turn kernel 骨架 — **已完成（`manage/workgroup/`）** |
| **D2** | Node WorkgroupWorker：强身份 WS、幂等 provision、tool manifest、command journal — **已完成（`node/internal/workgroup/`）** |
| **D3** | 纵向闭环：单组单成员单工具 + 信息型 HITL + 掉线/`indeterminate` + 重启 — **进行中（Timeline/HITL/`read_file` + WS hub/dispatch 骨架；生产长连接待续）** |
| **D4** | 多 Node 订阅与最小 UI 分栏 |
| **D5** | 拆除 Placement/Edge/remote stub/沙箱产品路径；拆旧 A2A |

**D0.5 未完成前禁止**并行实现 runtime 主路径（已满足）。D1/D2 骨架不含完整纵向工具闭环（属 D3）。

契约测试门槛（摘要）：tool-call 投影合法、伪装/保留名、并发 human 全序、provision 冲突、command 各阶段断线、非幂等不双执行、HITL CAS 与错误 Node 批准、fencing、WS replay、成员不可经本地 Agent API、Timeline 不泄 raw tool、旧 Placement 不能驱动成员。

最短纵向闭环与刻意不做：见 §16.8（历史审核，内容仍有效）。

---

## 14. 相关文档与吸收说明

| 文档 | 状态 |
|------|------|
| 本文 §0–§13 | **现行产品规范** |
| `workgroup-d05-contracts.md` | **D0.5 已冻结**（§19 Verdict A） |
| `fixtures/workgroup-d05/` | schemas + golden fixtures |
| `remote-agent-placement.md` | **superseded** |
| `node-centric-architecture-cleanup.md` | 继续：去 ops/compliance、去沙箱、拆旧 A2A |
| 下文 §15 / §16 | **历史审核记录**；冲突以正文为准 |

原 §14 自检补丁已吸收进正文：`tool.name` 分域、成员 LLM 在 Manage、独立 Worker、Registry、provision、HITL 分层、`indeterminate`、废除 Node 级 `agent_invoke`。本地 child/临时 Agent 仍为 Node 内能力，与工作组无关。

仍可实现期配置（非阻塞）：归档保留多久（默认不自动物理删）；ACL 外纯执行 Node（v1 不做）。

---

## 15. GPT 架构审核纪要（2026-07-30）〔历史〕

外部评审结论：**可做，但须先补契约（D0.5），不可直接全面开工。** 风险：**高**。

### 15.1 致命（已回流正文）

1. Timeline ≠ LLM history → §4 / §6  
2. Manage turn kernel → §5  
3. home Node 显式 opt-in → §8.3  
4. 掉线幂等 / indeterminate → §8.5  

### 15.2 重要补充（已回流）

稳定 id、幂等 provision、单 active assign、独立 Worker、Registry 可见性 → §6–§9。

### 15.3 开工顺序

见现行 §13。

---

## 16. 第二轮 GPT 架构审核（2026-07-30）〔历史〕

**Verdict 当时为 B（先改文档再开 D0.5）。** 正文已按 §16.7 回流；**下一门槛为 D0.5 契约文档/fixtures**，非再次改方向。

### 16.1–16.4 摘要

上一轮致命项未回流、`name` 编码、HITL 分层、Timeline 可见性、fencing、独立 Worker、HITL 闭合工具史等 → 已写入 §0 / §4–§11。

### 16.5 D0.5 增补清单

仍为开工检查表 — **已落成** [`workgroup-d05-contracts.md`](./workgroup-d05-contracts.md)；退出条件见该文 §13。

### 16.6 方向一致性

冻结方向无需推翻；「ACL 内 HITL」已限定为信息型。成员资产（§9）补强「不复用独聊继承」前提。

### 16.7 改文档清单

| 项 | 状态 |
|----|------|
| §0 决策表 | ✅ |
| §4 双层模型 | ✅ |
| §6 身份投影 | ✅ |
| §7 状态机 | ✅ |
| §8 ACL/Grant/HITL/fencing | ✅ |
| §9–§11 UI/WS | ✅（原 §9–10） |
| §13 分期 | ✅ |
| 成员侧车/记忆/工具 | ✅ |
| D0.5 契约文档 | ✅ 起草 [`workgroup-d05-contracts.md`](./workgroup-d05-contracts.md) |
| D0.5 fixtures / 退出评审 | ⏳ |

### 16.8 最短可行纵向闭环

**应包含：** 两 Node（A owner、B home）；ACL + B 对 MemberSpec **显式 Grant**；幂等 provision（不进本地 Agents API）；侧车/工具白名单来自 MemberSpec；human → Leader assign；Member 在 Manage + B 上 `read_file`；产出进 Timeline + 合法 tool 配对；信息型 HITL CAS；断线/`indeterminate`；归档 fencing；最小 UI。

**刻意不包含：** 多成员/并行 assign；background/Browser/媒体；skills/remember/child/A2A；已有 Agent 入组；raw tool 广播；Placement 物理删除；自动重做未知副作用。
