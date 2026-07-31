# 工作组 D0.5 契约（开工门槛）

> **状态**：**D0.5 已冻结**（§19 Verdict A）— D0.9–**D4 已完成**；**D5 进行中**（Cut6：列表自动归档 remote stub）；**禁止**跳过契约另开平行协议  
> **产品方向**：[`workgroup-and-node-gateway.md`](./workgroup-and-node-gateway.md)（§0–§13）  
> **本文件职责**：可测试的 schema、状态机、权限矩阵、投影规则、WS/工具恢复协议、威胁模型、旧数据处置、契约测试清单  
> **JSON Schema 附录**：[`fixtures/workgroup-d05/schemas/`](./fixtures/workgroup-d05/schemas/)  
> **规范优先级**：产品正文 §0–§13 > **本文（已冻结）** > schemas/*.json > 历史审核  
> **评审**：§17 B → §18 B（已补丁）→ **§19 A**
---

## 0. 范围与非目标

### 0.1 必须在 D0.5 冻结

| 块 | 内容 |
|----|------|
| A | ID / 版本字段 / 核心 JSON 形状 |
| B | 状态机与不变量（含 fencing） |
| C | 权限矩阵（ACL / Grant / HITL） |
| D | Timeline vs RunHistory vs Projector |
| E | MemberSpec 资产（侧车/记忆/工具绑组存 Manage） |
| F | ToolCommand 幂等与 `indeterminate` |
| G | WS 信封、resume、重复连接 |
| H | 威胁模型与旧 Placement 处置 |
| I | 契约测试门槛（可执行用例名） |

### 0.2 明确不在 D0.5

- Manage/Node 生产代码实现  
- 完整 UI、多成员并行 assign、Browser/media/skills  
- Placement 物理删除（仅处置策略）  
- 日历排期  

### 0.3 纵向闭环默认场景（契约以此为真）

两 Node：`node-A`（owner/订阅）+ `node-B`（home Worker）；单组单成员；首个 Node 工具 = `read_file`；一次信息型 HITL；断线与重启测 `indeterminate`。

---

## 1. 全局约定

### 1.1 ID 与字符集

| 字段 | 格式 | 说明 |
|------|------|------|
| `workgroup_id` | `wg_<ulid>`（**仅** 26 位小写 ulid，不用 UUID） | Manage 生成 |
| `member_id` | `mb_<ulid>` | Manage 预发；≠ 本地 `agt-…` |
| `actor_id` | `leader` \| `mb_<ulid>` \| `hu_<ulid>` \| 字面 `human` | 服务端盖章；Timeline 说话人 |
| `human_id` | `hu_<ulid>` | 同 Node 多展示名时稳定 id；未辨识可用字面 `human` 作为 actor_id |
| `run_id` / `turn_id` / `assign_id` | `rn_` / `tn_` / `as_` + **小写** ulid | `turn_id` = 单次模型往返；属 ActorRun |
| `event_id` | `ev_<ulid>` | Timeline 主键 |
| `seq` | uint64 | **Timeline 事件**每 workgroup 单调；Manage DB 事务内分配 |
| `delivery_seq` | uint64 | **WS 投递**单调序号（可跨类型）；与 Timeline `seq` **分离** |
| `tool_call_id` | provider 风格 id | 与 LLM 返回一致 |
| `command_id` | `cmd_<ulid>` | Manage 生成；Node journal 键 |
| `provision_id` | `pv_<ulid>` | 幂等 provision |
| `lease_id` | `ls_<ulid>` | |
| `lease_epoch` | uint64 | fencing；Grant/归档递增 |
| `member_generation` | uint64 | 成员规格世代；Command/Grant 必校验 |
| `connection_generation` | uint64 | 同 node_id 新连接递增；存在于 session 上下文与 ack |
| `client_message_id` | 客户端 **小写** ulid | human/HITL 去重 |
| `schema_version` | semver 字符串 | 信封与快照 |
| `payload_hash` | `sha256:` + 64 hex | 见 §1.4 |
| `tool_catalog_revision` | 非空字符串 | Node 上报工具清单版本 |
| `member_spec_digest` | `sha256:` + 64 hex | 见 §1.4 |

**ID 编码**：前缀后的 ulid **一律小写** `[0-9a-z]`；fixtures 与生产相同。  
**协议 `message.name`（provider-safe）**：`^[a-z][a-z0-9_]{0,63}$`  
允许：`leader` / `date` / `human` / `human_<id>` / `member_<id>` / `workgroup_assign`  
工具函数名亦须匹配同一字符集（现网工具名已满足）。  
**禁止**：display_name、中文、空格、冒号、任意用户输入直接入 `name`。

### 1.2 时间与错误

- **Wire 时间固定**：UTC RFC3339（例 `2026-07-30T07:00:00.000Z`）；内部存储可另选  
- 错误：`code`（§7.4）+ `message` + 可选 `retryable:bool`

### 1.3 权威存储

| 数据 | 权威 |
|------|------|
| ACL、Grant、MemberSpec、WorkGroupMember、Timeline、RunHistory、Assign、HITL | **Manage** |
| command journal、WorkerBinding、本地 policy 引擎 | **home Node** |
| WS | 传输；**不是**事实源 |

### 1.4 Canonical JSON 与 digest/hash

跨语言计算 `member_spec_digest` / `payload_hash`：

1. UTF-8 JSON  
2. 对象键 **字典序** 递归排序  
3. 无无意义空白（紧凑）  
4. 数字不改写；字符串按 JSON 标准转义  
5. 哈希输入 **不含** 自身 `digest`/`payload_hash` 字段  
6. 输出 `sha256:` + 小写 hex（64）

`ToolCommand.payload_hash` 覆盖：`tool_name`、`arguments_json`（规范后）、`member_id`、`assign_id`、`tool_call_id`、`member_spec_digest`、`member_generation`、`lease_epoch`、`tool_catalog_revision`。

---

## 2. Schema（逻辑 JSON）

> 字段名冻结。跨进程实体（schemas/*.json）**拒绝全部未知字段**（`additionalProperties: false`）。  
> Digest 输入另见 §1.4（不含自身 hash 字段）。`schema_version` 主版本不兼容则拒绝。

### 2.0 WorkGroupMember（Manage 权威）

```json
{
  "member_id": "mb_…",
  "workgroup_id": "wg_…",
  "home_node_id": "node_…",
  "display_name": "代码员",
  "status": "ready",
  "member_generation": 1,
  "member_spec_digest": "sha256:…",
  "active_assign_id": null,
  "created_at": "…",
  "archived_at": null
}
```

必填：除 `active_assign_id`/`archived_at` 外均必填。  
`memory.remember_enabled` **v1 必须为 false**；`skills`/`hooks` **必须为** `"disabled"`（见 MemberSpec）。

### 2.1 WorkGroup

```json
{
  "workgroup_id": "wg_…",
  "schema_version": "0.5.0",
  "display_name": "…",
  "status": "active",
  "created_by_node_id": "node-A",
  "llm_profile_id": "…",
  "llm_profile_revision": "…",
  "created_at": "…",
  "archived_at": null
}
```

### 2.2 WorkGroupACL

```json
{
  "workgroup_id": "wg_…",
  "owners": ["node-A"],
  "collaborators": ["node-B"],
  "revision": 3,
  "updated_at": "…"
}
```

### 2.3 NodeExecutionGrant

```json
{
  "grant_id": "gr_…",
  "workgroup_id": "wg_…",
  "member_id": "mb_…",
  "home_node_id": "node-B",
  "member_spec_digest": "sha256:…",
  "status": "accepted",
  "lease_id": "ls_…",
  "lease_epoch": 1,
  "member_generation": 1,
  "tool_allow_names": ["read_file"],
  "workspace_contract": {"root_kind": "member_workspace"},
  "policy_ceiling": {},
  "invited_at": "…",
  "accepted_at": "…",
  "revoked_at": null
}
```

### 2.4 MemberSpec（不可变快照；绑工作组存 Manage）

```json
{
  "member_id": "mb_…",
  "workgroup_id": "wg_…",
  "home_node_id": "node-B",
  "display_name": "代码员",
  "member_generation": 1,
  "llm_profile_id": "…",
  "llm_profile_revision": "…",
  "max_tool_loops": 32,
  "prompt": {
    "soul_md": "",
    "user_md": "",
    "custom_md": ""
  },
  "memory": {
    "remember_enabled": false,
    "initial_entries": []
  },
  "tools": {
    "allow_names": ["read_file"],
    "side_effect_classes": ["fs_read"]
  },
  "policy_ceiling": {},
  "workspace": {"root_kind": "member_workspace"},
  "skills": "disabled",
  "hooks": "disabled",
  "digest": "sha256:…"
}
```

规则：

- 创建时 Manage 计算 `digest`；之后只读  
- **禁止**只存 `template_id` 代替正文/工具名  
- **禁止** Node 从本机 `prompt_context` / 独聊模板继承  
- v1：`memory.remember_enabled` 必须 `false`；`skills`/`hooks` 必须 `"disabled"`  
- `memory.initial_entries`：允许显式种子，**最多 32** 条；默认 `[]`（不从 Node 迁移）  
- `tools.allow_names: []` = **无工具**（禁止回退 Node 默认组）

### 2.5 WorkerBinding（Node 本地）

```json
{
  "member_id": "mb_…",
  "workgroup_id": "wg_…",
  "home_node_id": "node-B",
  "provision_id": "pv_…",
  "member_spec_digest": "sha256:…",
  "lease_epoch": 1,
  "member_generation": 1,
  "workspace_path": "…",
  "status": "ready",
  "not_enumerable_as_local_agent": true
}
```

### 2.6 TimelineEvent

```json
{
  "event_id": "ev_…",
  "workgroup_id": "wg_…",
  "seq": 42,
  "type": "actor_final_text",
  "visibility": "subscribers",
  "actor_id": "mb_…",
  "actor_kind": "member",
  "authenticated_node_id": null,
  "protocol_name": "member_mb_…",
  "display_name_at_send": "代码员",
  "assign_id": "as_…",
  "turn_id": "tn_…",
  "content_text": "…",
  "created_at": "…"
}
```

`type` 枚举（v1）：`human_message` | `actor_final_text` | `assign_started` | `assign_finished` | `assign_indeterminate` | `system_notice` | `hitl_opened` | `hitl_resolved_summary`  

**禁止**默认写入 raw `tool_arguments` / `tool_result` 正文。

### 2.7 ActorRun / Turn / RunMessage

```json
{
  "run_id": "rn_…",
  "workgroup_id": "wg_…",
  "actor_id": "mb_…",
  "assign_id": "as_…",
  "status": "running",
  "llm_profile_revision": "…",
  "timeline_watermark_seq": 41,
  "checkpoint_ordinal": 7,
  "created_at": "…"
}
```

`ActorRun.status`：`running` | `awaiting_hitl` | `succeeded` | `failed` | `canceled` | `indeterminate`

```json
{
  "turn_id": "tn_…",
  "run_id": "rn_…",
  "ordinal": 3,
  "status": "open",
  "created_at": "…"
}
```

Turn = 单次模型往返（含其 tool_calls 收集）。`status`：`open` | `closed` | `aborted`

```json
{
  "run_id": "rn_…",
  "ordinal": 7,
  "turn_id": "tn_…",
  "role": "assistant",
  "protocol_name": "member_mb_…",
  "content": "…",
  "tool_calls": [{"id": "call_…", "type": "function", "function": {"name": "read_file", "arguments": "{…}"}}],
  "tool_call_id": null
}
```

`role=tool` 时：`tool_call_id` 必填；API `name` = **工具函数名**。  
唯一约束：`(run_id, ordinal)`；`(run_id, tool_call_id)` 对 tool 行唯一。

### 2.8 Assign

```json
{
  "assign_id": "as_…",
  "workgroup_id": "wg_…",
  "member_id": "mb_…",
  "leader_run_id": "rn_…",
  "leader_tool_call_id": "call_…",
  "status": "running",
  "instruction": "…",
  "result_summary": null,
  "created_at": "…"
}
```

不变量：**每组最多一个** `status ∈ {queued,running,awaiting_hitl}` 的 assign（v1）。

### 2.8.1 Manage-native 工具：`assign_workgroup_task`

机器可读全文：[`fixtures/workgroup-d05/schemas/assign_workgroup_task.openai.json`](./fixtures/workgroup-d05/schemas/assign_workgroup_task.openai.json)

**参数（LLM function）**

| 字段 | 必填 | 约束 |
|------|------|------|
| `member_id` | 是 | `^mb_[0-9a-z]{26}$` |
| `instruction` | 是 | 1…16000 字符 |
| `context_hint` | 否 | ≤2000；不替代 Projector 的 ≤10 Timeline 快照 |

**同步语义**：调用阻塞至该 assign 进入终态 `succeeded|failed|canceled|indeterminate`。

**tool result JSON（恰好一条，配对 `tool_call_id`）**

```json
{
  "assign_id": "as_…",
  "status": "succeeded",
  "summary": "成员最终文本或错误说明",
  "error_code": null
}
```

| status | summary | error_code |
|--------|---------|------------|
| `succeeded` | 成员最终产出文本 | `null` |
| `failed` / `canceled` / `indeterminate` | 人类可读原因 | 稳定 `error_code`（§7.4） |

与 Timeline：可另写 `actor_final_text`；Leader 上下文按 `assign_id` **去重**，只保留本 tool result。

### 2.9 ToolCommand / ToolResult

```json
{
  "command_id": "cmd_…",
  "workgroup_id": "wg_…",
  "member_id": "mb_…",
  "assign_id": "as_…",
  "run_id": "rn_…",
  "turn_id": "tn_…",
  "tool_call_id": "call_…",
  "tool_name": "read_file",
  "arguments_json": "{…}",
  "payload_hash": "sha256:…",
  "lease_id": "ls_…",
  "lease_epoch": 1,
  "member_generation": 1,
  "member_spec_digest": "sha256:…",
  "tool_catalog_revision": "rev_…",
  "status": "queued",
  "side_effect_class": "fs_read"
}
```

有效工具集 = `MemberSpec.allow_names ∩ Grant.tool_allow_names ∩ Node.manifest ∩ 本地更严 policy`。  
Grant 字段若存在：必须 ⊆ Spec；否则 `digest_mismatch`/`not_authorized`。

```json
{
  "command_id": "cmd_…",
  "status": "succeeded",
  "result_text": "…",
  "is_error": false,
  "error_code": null,
  "finished_at": "…"
}
```

### 2.10 HITLRequest / HITLResolution

```json
{
  "hitl_id": "ht_…",
  "workgroup_id": "wg_…",
  "kind": "information_request",
  "actor_run_id": "rn_…",
  "turn_id": "tn_…",
  "tool_call_id": "call_…",
  "command_id": null,
  "payload_hash": null,
  "status": "pending",
  "prompt": "…",
  "home_node_id": "node_…"
}
```

`kind=execution_approval` 时：`command_id` + `payload_hash` **必填**，绑定不可变命令。

```json
{
  "hitl_id": "ht_…",
  "resolution_id": "hr_…",
  "resolver_node_id": "node_…",
  "client_message_id": "…",
  "decision": "answered",
  "answer_text": "…",
  "cas_version": 1,
  "resolved_at": "…"
}
```

`decision`：信息型用 `answered`（须 `answer_text`）；执行批准用 `approved` | `denied`（无须把批准伪造成工具成功）。

### 2.11 Subscription / ResumeCursor

```json
{
  "workgroup_id": "wg_…",
  "node_id": "node-A",
  "desired": true,
  "updated_at": "…"
}
```

```json
{
  "node_id": "node-A",
  "workgroup_id": "wg_…",
  "last_ack_delivery_seq": 41,
  "connection_generation": 5
}
```

### 2.12 WSEnvelope / CommandAck / ArchiveTombstone

```json
{
  "envelope_id": "en_…",
  "schema_version": "0.5.0",
  "type": "timeline.event",
  "workgroup_id": "wg_…",
  "delivery_seq": 42,
  "connection_generation": 5,
  "payload": {},
  "sent_at": "…"
}
```

`type` 前缀：`session.*` | `workgroup.*` | `member.*` | `tool.*` | `hitl.*` | `timeline.*` | `resume.*`

```json
{
  "command_id": "cmd_…",
  "status": "accepted",
  "connection_generation": 5,
  "journaled_at": "…"
}
```

```json
{
  "workgroup_id": "wg_…",
  "member_id": "mb_…",
  "lease_epoch_at_archive": 4,
  "archived_at": "…"
}
```

---

## 3. 状态机

### 3.1 WorkGroup

```text
active → archiving → archived
```

- `archiving`：拒绝新 assign / provision；进行中 assign 取消或等到终态  
- `archived`：只读 Timeline；所有 Grant `revoked`；`lease_epoch` 递增  

### 3.2 ExecutionGrant

```text
invited → accepted → revoked
         ↘ revoked（邀请过期/拒绝）
```

- 仅 `accepted` 可 provision / 收 tool command  
- `revoked` 后旧 `lease_epoch` 的 command **一律拒绝**  

### 3.3 Member

```text
requested → provisioning → ready ⇄ busy → archived
                       ↘ error
ready / busy → archived
```

- `busy`：存在 active assign  
- 归档后不可再 `@`  

### 3.4 Assign

```text
queued → running → awaiting_hitl → running → succeeded
                              ↘ failed | canceled | indeterminate
         running → failed | canceled | indeterminate
```

### 3.5 ToolCommand

```text
queued → accepted → running → succeeded | failed | canceled | indeterminate
queued → rejected
accepted → running          # journal owner 恢复未开始的执行
accepted → indeterminate    # 仅当副作用可能已开始且结果未知
```

**`accepted`**：Node 已将 command **持久化进 journal 之后**才可返回 ack。  

重复投递同 `command_id`：

- 返回 journal 既有状态；**不得**第二次执行  
- `payload_hash` 不一致 → `payload_conflict`  

恢复：

- journal=`accepted` 且副作用**未开始**：同一 journal owner **可恢复执行一次**  
- 副作用**已开始**结果未知 → `indeterminate`；禁止换新 id 自动重做  
- journal 已终态、ack 丢失：重放终态结果  

归档时：`queued`→`canceled`；`accepted`（未开始）→`canceled`；`running`/可能有副作用→`indeterminate`（不得伪造成无副作用的 canceled）。

### 3.6 HITL

```text
pending → resolved | expired | canceled
```

CAS：仅一个 `resolution` 成功；失败方 `already_resolved`。

按 `kind`：

| kind | 谁可 resolve | 成功后 |
|------|--------------|--------|
| `information_request` | ACL 内订阅者 | **恰好一次**写入 RunHistory tool result（答案文本），再 resume run |
| `execution_approval` | 仅 home | `approved`→执行**已绑定** `command_id`（校验 hash）；工具结果闭合原 `tool_call_id`。`denied`/`expired`→合成错误 tool result，**不执行** |

原子边界：resolution 行提交 + tool result 插入 + resume enqueue 同一 Manage 事务（或等价 exactly-once outbox）。

### 3.7 Fencing 顺序（归档 / 撤权）

1. Manage：置组/成员归档或 Grant `revoked`，`lease_epoch++`，写 `ArchiveTombstone`  
2. Manage outbox：通知 Node  
3. Node：拒收旧 epoch command；停 Worker；可选清工作区  
4. 延迟到达的旧 WS 帧：校验 generation/epoch 失败则丢弃  

---

## 4. 权限矩阵

| 动作 | wg_owner | wg_collaborator | home（Grant accepted） | 其他 |
|------|----------|-----------------|------------------------|------|
| 改 ACL / 解散 | ✅ | ❌ | ❌ | ❌ |
| 发起成员邀请 / 归档成员 | ✅ | ❌ | ❌ | ❌ |
| 接受/拒绝 Grant | ❌ | ❌ | ✅（目标 Node） | ❌ |
| 订阅 / 读 Timeline | ✅ | ✅ | ✅（若亦在 ACL） | ❌ |
| 发 human | ✅ | ✅ | 若在 ACL | ❌ |
| 信息型 HITL resolve | ✅ | ✅ | ✅（若在 ACL） | ❌ |
| 执行批准 HITL | ❌* | ❌* | ✅ | ❌ |
| 执行 tool command | — | — | ✅（digest/epoch 匹配） | ❌ |
| 枚举全网 Node | platform_admin 或授权过滤 | | | 默认 ❌ |

\* 除非 home **显式委托**（v1 不做委托）。

「在 ACL」≠「可在该机跑 Shell」。

---

## 5. Timeline / RunHistory / Projector

### 5.1 分层

| 层 | 喂谁 | 含 tool_calls？ |
|----|------|-----------------|
| Timeline | 订阅 UI / 审计摘要 | 否（默认） |
| ActorRunHistory | 该 Actor 的 LLM loop | 是（必须合法配对） |
| Projector 输出 | 另一 Actor 的 LLM | 他者产出投影为外部输入；本 Actor 历史原样 |

### 5.2 投影规则（冻结 · 确定函数）

对 Actor `X` 构造上下文：

1. 取 `X` 的 RunHistory 至当前 checkpoint（含未完成 tool 配对）  
2. Timeline 快照窗口：最近 **≤10** 条、且 `seq > run.timeline_watermark_seq` 的**其他** actor 已提交事件  
3. 若本 Actor 存在 **open tool_calls**：新 Timeline 事件进入 **buffer**，不得插入 LLM 上下文，直到本轮 tool 全部配对或 turn aborted  
4. buffer/`≤10` 事件投影为：`role=user`，`name=<protocol_name>`，`content=content_text`（按 `seq` 升序）  
5. Member 最终产出同时作为 Leader `assign_workgroup_task` 的 tool result：Leader **只保留 tool 配对**；同 `assign_id` 的 Timeline 不再插入 user  
6. Assign 终态必须给 Leader **恰好一条** tool result：`succeeded`→摘要文本；`failed`/`canceled`/`indeterminate`→`is_error` 合成结果（含 `error_code`）

### 5.3 并行 tool_calls（冻结一种）

v1：**等待全部结果**后再续写模型；禁止半配齐续写。  
任一 call 超时/失败：为其补 **错误 tool result**（`is_error=true`），然后 **闭 turn**（不 aborted）；再决定是否开新 turn。

### 5.4 Golden fixtures（目录约定）

```text
docs/design/fixtures/workgroup-d05/
  INDEX.json                 # test → file 索引
  projection/                # LLM 投影与 tool 配对
  identity/                  # provider-safe name / 保留名
  messaging/                 # human 去重与全序
  provision/                 # 幂等 provision
  tool_command/              # 幂等 / indeterminate
  hitl/                      # CAS 与审批权威
  fencing/                   # 归档与 ACL 撤销
  ws/                        # resume / 双连 / 重启
  catalog/                   # tool catalog drift
  security/                  # Timeline 泄露、旧 Placement、fail-closed 工具
  member_api/                # 不进本地 Agents / 禁独聊
```

完整映射见 `INDEX.json` 与下文 §12。
---

## 6. MemberSpec 资产契约

与产品正文 §9 一致，契约级重申：

| 资产 | Manage（随组） | Node |
|------|----------------|------|
| 侧车 | Spec.prompt.* | 不落权威副本 |
| 记忆 | Manage；v1 `remember_enabled=false` | 不写 longterm_store |
| 工具权限 | `allow_names` fail-closed | 执行 ∩ catalog ∩ 本地更严 policy |
| LLM | 云档案 | 不驱动成员 turn |
| 工作区文件 | 契约 | 执行副作用；非人格 |

组 `archived` → Spec/记忆/RunHistory 只读归档；WorkerBinding 回收。

---

## 7. 工具与恢复协议

### 7.1 Node tool manifest

```json
{
  "node_id": "node-B",
  "tool_catalog_revision": "…",
  "tools": [
    {
      "name": "read_file",
      "json_schema": {},
      "side_effect_class": "fs_read",
      "execution_mode": "sync"
    }
  ]
}
```

Manage 在发 command 前校验：`tool_name ∈ Spec.allow_names ∩ manifest`；revision 漂移 → 拒绝或要求刷新（不得静默用旧 schema）。

### 7.2 幂等与恢复

| 阶段 | 行为 |
|------|------|
| 未 accepted | 可重发同一 `command_id`+hash |
| 已 accepted，副作用未开始 | journal owner 恢复执行；Manage 重发只查状态 |
| 已 running / 结果未知 | → `indeterminate`；禁止新 id 重做非幂等 |
| 终态 | 返回终态；可重放 result |

### 7.3 Manage / Node 持久化边界

- Manage：Assign、RunHistory、outbox（待投递 command / resume）  
- Node：journal（command_id → 状态/结果）  
- journal 丢失且可能有副作用 → `indeterminate`

### 7.4 错误码（稳定）

| code | 含义 |
|------|------|
| `not_authorized` | ACL/Grant |
| `fencing_rejected` | epoch/generation |
| `digest_mismatch` | MemberSpec |
| `catalog_drift` | 工具清单 |
| `payload_conflict` | 同 id 不同 hash |
| `already_resolved` | HITL CAS |
| `policy_denied` | 本地否决 |
| `schema_mismatch` | 参数 |
| `result_too_large` | |
| `indeterminate` | 结果未知 |
| `workgroup_archived` | |
| `duplicate_client_message` | 同 client_message_id **且** payload 不同以外的重复语义保留；同 payload 重试应幂等返回原事件（用原 event，不报此码） |
| `cursor_too_old` | WS resume 超出保留窗口 |
| `not_found` | 资源不存在（含「成员非本地 Agent」） |
| `conflict` | 通用冲突（非 payload_hash 专用时） |

---

## 8. WS 契约

### 8.1 会话

1. Node 用**独立凭据**连接（fail-closed）  
2. `session.hello` → Manage 确认 `connection_generation`  
3. 同 `node_id` 新连接成功 → 旧 generation **立即 fencing**  
4. `resume.offer(last_ack_delivery_seq)` → 从 outbox 按 `delivery_seq` gap-fill  
5. 若 cursor 早于保留窗口 → `cursor_too_old`（客户端先拉快照再 resume）

### 8.2 序号

| 字段 | 含义 |
|------|------|
| Timeline `seq` | 仅 TimelineEvent |
| `delivery_seq` | 所有需可靠投递的 WS 业务帧（timeline/tool/hitl/provision…） |

Ack 确认的是 `delivery_seq`。Generation 校验依赖**连接上下文**（不必每帧重复带齐，但 CommandAck 须带回 generation）。

### 8.3 最小消息目录

| type | 方向 | 说明 |
|------|------|------|
| `session.hello` / `session.welcome` | ↔ | 鉴权与 generation |
| `resume.offer` / `resume.batch` / `resume.complete` | ↔ | gap-fill |
| `timeline.event` | Manage→Node | payload=TimelineEvent |
| `tool.command` / `tool.ack` / `tool.result` | ↔ | |
| `member.provision` / `member.provision_result` | ↔ | |
| `hitl.request` / `hitl.resolve` | ↔ | |
| `workgroup.tombstone` | Manage→Node | 归档 fencing |

### 8.4 背压与 human 入队

- 超出窗口未 ack：暂停非关键扇出（实现数值 D1 可调）  
- Leader 同步 `@` 期间：新 human **入队**，结束后按 Timeline `seq` 喂入  

### 8.5 与事实源

断线丢的是帧；重连靠 `delivery_seq` 补洞。

---

## 9. 威胁模型（摘要）

| 威胁 | 缓解 |
|------|------|
| ACL 加机器即可远程 Shell | ExecutionGrant + 本地 policy + digest |
| 伪造 leader/成员名 | provider-safe name + 服务端 actor_id；保留名不可抢 |
| Timeline 泄露 Shell/密钥 | 禁止 raw tool 进 Timeline |
| 重放旧 command | lease_epoch / tombstone |
| 双连接双执行 | connection_generation |
| 开放模式误用 | 工作组 WS 独立凭据 |
| 独聊 API 双脑 | Worker 不进 agents 表；封锁本地 Agent API |
| Registry 侦察 | 目录授权过滤 |
| 同 command 双执行 | journal 幂等 |

---

## 10. 旧 Placement / A2A 处置

| 项 | D0.5 策略 |
|----|-----------|
| 新产品入口（UI 选 peer、placement 开关） | **D0.9 停写**；文档标明 deprecated |
| 已有 remote stub / Edge 聊天 | 不接入工作组；D5 删除 |
| 旧 A2A inbox/`agent_invoke` | 不作为成员驱动；契约测试断言无法驱动 `mb_*` |
| 旧数据 | 不自动迁移成 Member；需人工重建组 |
| `discovery_group` | 不作 ACL/Grant 依据 |

---

## 11. 纵向闭环验收（契约视角）

按序全部为真才算 D3 可测：

1. A 建组；B∈ACL；B 接受 Grant(digest)  
2. provision 幂等；`GET /v1/agents` **无**该成员  
3. Spec 侧车/工具在 Manage；B 无权威 soul 副本  
4. human → Leader assign → Member loop → `read_file` on B  
5. Timeline 有最终产出；无 raw tool body  
6. Leader tool result 合法配对且不与 Timeline 重复计双份  
7. 信息型 HITL 双端 CAS 仅一次恢复  
8. accepted 前后杀进程 → 不双执行 / 或 `indeterminate`  
9. 归档后旧 command fencing  
10. 任意本地 messages/agents API 操作成员 → 明确错误  

---

## 12. 契约测试清单（用例名 → fixture）

| # | test | fixture |
|---|------|---------|
| 1 | `projection_openai_legal_sequence` | `projection/openai_member_sees_leader.json` + `tool_name_not_overridden.json` + `parallel_tool_calls_must_pair_all.json` |
| 2 | `projection_deepseek_assistant_name_fallback` | `projection/deepseek_leader_sees_member_final.json` |
| 3 | `protocol_name_rejects_unicode_display` | `identity/reject_display_name_as_protocol_name.json` |
| 4 | `reserved_name_spoof_rejected` | `identity/reserved_name_spoof.json` |
| 5 | `human_client_message_id_dedupe` | `messaging/human_client_message_id_dedupe.json` |
| 6 | `concurrent_human_total_order_by_seq` | `messaging/concurrent_human_total_order_by_seq.json` |
| 7 | `provision_retry_same_id_ok` | `provision/retry_same_id_ok.json` |
| 8 | `provision_same_id_different_digest_conflict` | `provision/same_id_different_digest_conflict.json` |
| 9 | `tool_cmd_resend_before_accept` | `tool_command/resend_before_accept.json` |
| 10 | `tool_cmd_no_reexec_after_accept` | `tool_command/no_reexec_after_accept.json` |
| 11 | `non_idempotent_handler_executes_once` | `tool_command/non_idempotent_handler_executes_once.json` |
| 12 | `indeterminate_on_journal_loss_after_accept` | `tool_command/indeterminate_on_journal_loss_after_accept.json` |
| 13 | `archive_fencing_rejects_stale_epoch` | `fencing/archive_rejects_stale_epoch.json` |
| 14 | `acl_revoke_stops_timeline_read` | `fencing/acl_revoke_stops_timeline_read.json` |
| 15 | `hitl_double_resolve_cas` | `hitl/double_resolve_cas.json` |
| 16 | `hitl_execution_approval_wrong_node_denied` | `hitl/execution_approval_wrong_node_denied.json` |
| 17 | `hitl_exactly_once_tool_result_resume` | `hitl/exactly_once_tool_result_resume.json` |
| 18 | `manage_restart_pending_assign` | `ws/manage_restart_pending_assign.json` |
| 19 | `node_restart_each_persist_boundary` | `ws/node_restart_each_persist_boundary.json` |
| 20 | `ws_gap_fill_and_dup_connection_fence` | `ws/gap_fill_and_dup_connection_fence.json` |
| 21 | `tool_catalog_revision_drift` | `catalog/tool_catalog_revision_drift.json` |
| 22 | `member_not_in_local_agents_api` | `member_api/not_in_local_agents_api.json` |
| 23 | `member_local_messages_api_rejected` | `member_api/local_messages_api_rejected.json` |
| 24 | `timeline_excludes_raw_tool_payload` | `security/timeline_excludes_raw_tool_payload.json` |
| 25 | `legacy_placement_cannot_drive_member` | `security/legacy_placement_cannot_drive_member.json` |
| 26 | `empty_allow_names_means_no_tools` | `security/empty_allow_names_means_no_tools.json` |
| 27 | `node_policy_can_only_tighten` | `security/node_policy_can_only_tighten.json` |
| 28 | `assign_result_deduped_vs_timeline` | `projection/assign_result_deduped_vs_timeline.json` |
| 29 | `vertical_two_node_read_file_happy_path` | `vertical/two_node_read_file_happy_path.json` |
| 30 | `acl_without_grant_denied` | `grant/acl_without_grant_denied.json` |
| 31 | `digest_generation_fenced` | `grant/digest_generation_fenced.json` |
| 32 | `tool_cmd_recover_accepted_before_running` | `tool_command/recover_accepted_before_running.json` |
| 33 | `tool_cmd_result_persisted_reply_lost` | `tool_command/result_persisted_reply_lost.json` |
| 34 | `open_tool_call_buffers_timeline` | `projection/open_tool_call_buffers_timeline.json` |
| 35 | `hitl_execution_approval_payload_bound` | `hitl/execution_approval_payload_bound.json` |
| 36 | `ws_cursor_too_old_resync` | `ws/cursor_too_old_resync.json` |
| 37 | `member_assets_manage_authoritative` | `member_assets/manage_authoritative.json` |

Fixture 元格式：`fixture_schema=workgroup-d05-fixture/v1`；期望值禁止 `or` / `reject_or_*` 二选一。

---

## 13. 完成检查表（退出 D0.5）

- [x] §2 + [`schemas/`](./fixtures/workgroup-d05/schemas/) JSON Schema 附录（核心类型 + assign 工具 + WS/HITL/Assign 等）  
- [x] §2.8.1 `assign_workgroup_task` 参数/结果 schema（含 succeeded⇒error_code null）  
- [x] §2.11–§2.12 / §8 统一 `delivery_seq`  
- [x] §3 / §5.3 状态与并行收束无二选一  
- [x] §4 矩阵与产品正文一致  
- [x] §7 错误码含 `cursor_too_old` / `not_found`  
- [x] §9–§10 威胁与旧数据策略已写入  
- [x] §12 fixtures：合法 26 位 ulid、given/when/then、INDEX 39 条  
- [x] GPT 复审 **Verdict A**（§19）  
- [x] 产品正文交叉引用本文件为 **D0.5 已冻结**  
- [x] **未**合并未门禁的 Manage turn kernel / Worker 大改  

**退出后下一动作**：D0.9→D1→D2→D3 基座（**已完成**）→ **D4** 多 Node 订阅与最小 UI。

---

## 14. 修订记录

| 日期 | 说明 |
|------|------|
| 2026-07-30 | 首稿 |
| 2026-07-30 | fixtures + INDEX；§17 Verdict B 最短补丁 |
| 2026-07-30 | schemas/、assign 工具、fixtures given/when/then |
| 2026-07-30 | §18 Verdict B；按 18.5 打补丁 |
| 2026-07-30 | §19 Verdict A；清残留字段名；**D0.5 冻结** |

---

## 15. 附录 A：JSON Schema 文件索引

路径：`docs/design/fixtures/workgroup-d05/schemas/`

| 文件 | 实体 |
|------|------|
| `defs.json` | ID / hash / error 共享定义 |
| `WorkGroup.json` / `WorkGroupACL.json` / `WorkGroupMember.json` | 组 |
| `MemberSpec.json` / `NodeExecutionGrant.json` / `WorkerBinding.json` | 规格与执行 |
| `TimelineEvent.json` / `ActorRun.json` / `Assign.json` | 运行 |
| `ToolCommand.json` / `ToolResult.json` | 工具 |
| `HITLRequest.json` / `HITLResolution.json` | HITL |
| `ResumeCursor.json` / `WSEnvelope.json` / `CommandAck.json` / `ArchiveTombstone.json` | WS |
| `assign_workgroup_task.openai.json` | Leader 工具（唯一机器契约） |
| `FixtureMeta.json` | fixture 信封 |

---

## 17. D0.5 契约 GPT 评审（2026-07-30）〔历史 · Verdict B〕

**Verdict：B — 须修订后再冻结**（本轮已按最短补丁集改契约与 fixtures；**须再审一次**方可改 A）。

### 17.1 总评

方向与产品正文一致，无需回到 Placement/Node LLM。阻塞点是：示例 JSON ≠ 可判定契约、投影/WS/HITL/恢复允许多实现并存、fixtures 大量二选一。已开始收紧；尚未宣称冻结。

### 17.2 一致性

一致：工作组唯一跨 Agent 路径；资产绑 Manage；ACL≠Grant；Timeline≠RunHistory；信息型 vs 执行批准；幂等+indeterminate；provider-safe name。

原稿缺口（已修）：`member_generation` 校验链、≤10 快照、`actor_id` 含 `hu_*`、catalog revision 上 command、HITL 按 kind 闭合工具史。

### 17.3 致命项处置

| 项 | 处置 |
|----|------|
| Schema 不可独立验证 | 部分：§1.4 hash、未知字段规则；**仍欠**完整 JSON Schema 附录 |
| 缺 Member/Turn/Run 恢复模型 | ✅ §2.0 / §2.7 |
| Projector 非确定 | ✅ §5.2–5.3 wait-all + buffer + ≤10 |
| ToolCommand 安全字段不足 | ✅ generation + catalog revision；恢复语义 §3.5/§7.2 |
| WS seq 歧义 | ✅ `delivery_seq` 分离 + 消息目录 |
| 执行批准 TOCTOU | ✅ command/hash 绑定；approved 执行 / denied 合成错误 |

### 17.4 Fixtures

- 原 28 用例已映射；二选一文件已改写为单一期望  
- 新增 vertical/grant/recover/open-tool/approval-bound/cursor/member_assets  
- **仍非**最终跨语言 golden：缺统一机器 comparator 与完整 given 状态图；复审聚焦可执行性  

### 17.5 冻结前剩余（首轮）

1. ~~附录：JSON Schema~~ → §15 / `schemas/`  
2. ~~`assign_workgroup_task` schema~~ → §2.8.1  
3. ~~fixtures 规范化~~ → given/when/then  
4. 复审 GPT → §18

---

## 18. D0.5 契约 GPT 复审（2026-07-30）

**Verdict：B** — 产品方向一致，但 WS 序号、HITL/assign 条件约束及 fixtures 可执行性当时未闭环，**不能冻结**。

> **注：** 本 Verdict 针对复审当时快照。其后已按 §18.5 打补丁（见修订记录）；是否升为 A 须 **§19 第三轮确认** 或人工签核。

### 18.1 首轮致命项（复审当时）

| 项 | 当时 | 补丁后（预期） |
|----|------|----------------|
| Schema 可独立验证 | 仍开 | schemas 已补 Assign/WS/HITLResolution/WorkerBinding/ActorRun 等 |
| Member/Turn/Run | 已关闭 | — |
| Projector | 仍开（§5.3 二选一） | §5.3 仅「补错误结果 + 闭 turn」 |
| ToolCommand 恢复 | 已关闭 | — |
| delivery_seq | 仍开（示例用 seq） | §2.11–2.12 已改 `delivery_seq` |
| 执行批准 TOCTOU | 仍开（schema 允 null） | HITLRequest 条件强制非空 hash |
| Fixtures 可执行 | 仍开 | 39 条合法 ulid + FixtureMeta 必填字段 |

### 18.2 Schema 与正文冲突（复审当时 → 处置）

1. 旧 ResumeCursor 投递游标字段名 vs `delivery_seq` → ✅ 已统一为 `last_ack_delivery_seq`  
2. Grant 缺 `member_generation` 示例 → ✅  
3. workgroup_id UUID vs ULID → ✅ 仅 ULID  
4. 未知字段策略 → ✅ 跨进程实体全部拒绝未知字段  
5. HITL execution null → ✅ schema allOf  
6. assign result 条件 → ✅ oneOf succeeded/error  
7. 错误码缺 cursor_too_old/not_found → ✅ §7.4  
8. initial_entries maxItems 0 → ✅ 最多 32  
9. 缺核心 schemas → ✅ 已补  
10. 双 assign 工具文件 → ✅ `.tool.json` 标 deprecated，以 `.openai.json` 为准  

### 18.3 Fixtures（复审当时）

不能直接当跨语言 golden。补丁后：全部含 given/when/then；ID 26 位校验通过；仍缺通用 op 词汇表实现与自动 JSON Schema 校验 CI（可 D1 早期补）。

### 18.4 冻结声明

复审当时：**不适用**。补丁后：**仍勿自行标冻结**，等 §19 或人工签核。

### 18.5 必须补丁清单（已执行）

1. ✅ delivery_seq / ResumeCursor / WSEnvelope  
2. ✅ Grant `member_generation`  
3. ✅ HITLRequest 非空 command/hash + HITLResolution.json  
4. ✅ assign result 条件不变量  
5. ✅ 补齐核心 schemas  
6. ✅ ID / 未知字段 / 错误码对齐  
7. ✅ initial_entries v1 语义  
8. ✅ §5.3 单一收束  
9. ✅ FixtureMeta 收紧；fixtures 全量迁移  
10. ✅ 合法 ID/hash；INDEX 39  

### 18.6 后续阶段（复审当时结论）

- D0.9 / D1：**当时不能开始**  
- 方向约束「不得新增 Placement 产品能力」持续有效  
- 补丁后建议：先跑 §19 快审；若 A，则 D0.9 可开、D1 骨架可开  

---

## 19. D0.5 契约 GPT 第三轮确认（2026-07-30）

**Verdict：A** — §18.5 十项已闭环；唯一阻塞为历史段落残留旧游标字段名，已改为「旧 ResumeCursor 投递游标字段名」表述。

### 19.1 §18.5 关闭情况

1. ✅ `delivery_seq` / `last_ack_delivery_seq` 统一  
2. ✅ Grant `member_generation`  
3. ✅ HITL execution 非空 command/hash + HITLResolution  
4. ✅ assign result oneOf  
5. ✅ 核心 schemas 齐备  
6. ✅ ID / 未知字段 / 错误码对齐  
7. ✅ `initial_entries` ≤32  
8. ✅ §5.3 单一收束  
9. ✅ fixtures given/when/then + FixtureMeta  
10. ✅ 合法 ID/hash；INDEX 39  

### 19.2 阻塞补丁

无（历史表述已清理）。

### 19.3 允许开始 / 延后

| 可开始 | 延后 |
|--------|------|
| **D0.9** 停 Placement/sandbox 新产品入口 | Placement 物理删除（D5） |
| **D1** Manage 组存储 / ACL / Grant / MemberSpec / turn kernel **骨架** | 完整 UI、多成员并行、Browser/skills |
| 契约测试 harness 雏形（读 fixtures） | 背压数值、保留期等调参 |

### 19.4 冻结声明

**可以将本文档状态标为「D0.5 已冻结」。** 实现须遵守本文 + `schemas/`；变更走修订记录，不得静默分叉协议。
