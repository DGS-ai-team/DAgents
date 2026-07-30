# 工作组 D0.5 契约（开工门槛）

> **状态**：起草中 → 评审通过后冻结为 D1 实现依据  
> **产品方向**：[`workgroup-and-node-gateway.md`](./workgroup-and-node-gateway.md)（§0–§13）  
> **本文件职责**：可测试的 schema、状态机、权限矩阵、投影规则、WS/工具恢复协议、威胁模型、旧数据处置、契约测试清单  
> **规范优先级**：产品正文 §0–§13 > **本文** > 历史审核 §15/§16  
> **完成定义**：下列各章无「TBD」阻塞项，且 §12 契约测试用例可写成跨语言 fixtures 目录结构

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
| `workgroup_id` | `wg_<ulid/uuid>` | Manage 生成 |
| `member_id` | `mb_<ulid>` | Manage 预发；≠ 本地 `agt-…` |
| `actor_id` | `leader` 或 `mb_<…>` | 服务端盖章 |
| `human_id` | `hu_<ulid>` | 同 Node 多展示名时稳定 id；未辨识可用字面 `human` |
| `run_id` / `turn_id` / `assign_id` | `rn_` / `tn_` / `as_` + ulid | |
| `event_id` | `ev_<ulid>` | Timeline 主键 |
| `seq` | uint64 | **每 workgroup 单调**；Manage DB 事务内分配 |
| `tool_call_id` | provider 风格 id | 与 LLM 返回一致 |
| `command_id` | `cmd_<ulid>` | Manage 生成；Node journal 键 |
| `provision_id` | `pv_<ulid>` | 幂等 provision |
| `lease_id` | `ls_<ulid>` | |
| `lease_epoch` | uint64 | fencing；Grant/归档递增 |
| `connection_generation` | uint64 | 同 node_id 新连接递增 |
| `client_message_id` | 客户端 ulid | human/HITL 去重 |
| `schema_version` | semver 字符串 | 信封与快照 |
| `payload_hash` | `sha256:<hex>` | 命令体规范 JSON 哈希 |
| `tool_catalog_revision` | 字符串/哈希 | Node 上报工具清单版本 |
| `member_spec_digest` | `sha256:<hex>` | MemberSpec 规范序列化哈希 |

**协议 `message.name`（provider-safe）**：`^[a-z][a-z0-9_]{0,63}$`  
允许：`leader` / `date` / `human` / `human_<id>` / `member_<id>` / `workgroup_assign`  
**禁止**：display_name、中文、空格、冒号、任意用户输入直接入 `name`。

### 1.2 时间与错误

- 时间一律 UTC RFC3339 / 毫秒 epoch（实现二选一，fixtures 用 RFC3339）  
- 错误类型稳定枚举（见 §7.4）；对客户端给 `code` + `message` + 可选 `retryable`

### 1.3 权威存储

| 数据 | 权威 |
|------|------|
| ACL、Grant、MemberSpec、Timeline、RunHistory、Assign、HITL | **Manage** |
| command journal、WorkerBinding、本地 policy 引擎 | **home Node** |
| WS | 传输；**不是**事实源 |

---

## 2. Schema（逻辑 JSON）

> 下列为契约形状，非最终 ORM。字段名冻结；可加可选字段但不得改变必填语义。

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

### 2.5 WorkerBinding（Node 本地）

```json
{
  "member_id": "mb_…",
  "workgroup_id": "wg_…",
  "home_node_id": "node-B",
  "provision_id": "pv_…",
  "member_spec_digest": "sha256:…",
  "lease_epoch": 1,
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

### 2.7 ActorRun / RunMessage

```json
{
  "run_id": "rn_…",
  "workgroup_id": "wg_…",
  "actor_id": "mb_…",
  "assign_id": "as_…",
  "status": "running",
  "llm_profile_revision": "…",
  "created_at": "…"
}
```

```json
{
  "run_id": "rn_…",
  "ordinal": 7,
  "role": "assistant",
  "protocol_name": "member_mb_…",
  "content": "…",
  "tool_calls": [{"id": "call_…", "type": "function", "function": {"name": "read_file", "arguments": "{…}"}}],
  "tool_call_id": null
}
```

`role=tool` 时：`tool_call_id` 必填；`protocol_name` / API `name` = **工具函数名**。

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
  "member_spec_digest": "sha256:…",
  "status": "queued",
  "side_effect_class": "fs_read"
}
```

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
  "status": "pending",
  "prompt": "…",
  "home_node_id": "node-B"
}
```

```json
{
  "hitl_id": "ht_…",
  "resolution_id": "hr_…",
  "resolver_node_id": "node-A",
  "client_message_id": "…",
  "answer_text": "…",
  "cas_version": 1,
  "resolved_at": "…"
}
```

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
  "last_ack_seq": 41,
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
  "seq": 42,
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
accepted → indeterminate（journal 在、结果未知）
```

**`accepted` 定义**：Node 已将 command **持久化进 journal 之后**才可返回 ack。  
同 `command_id`：返回既有状态；`payload_hash` 不一致 → `conflict`（不执行）。

### 3.6 HITL

```text
pending → resolved | expired | canceled
```

CAS：仅一个 `resolution` 成功；失败方得 `already_resolved`。  
成功后：**恰好一次**写入对应 RunHistory 的 tool result，再恢复 run。

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

### 5.2 投影规则（冻结）

对 Actor `X` 构造上下文：

1. 取 `X` 的 RunHistory（含未完成 tool 配对）  
2. 合并 Timeline 中 **其他** actor 的已提交事件，投影为：  
   - `role=user`  
   - `name=<protocol_name>`（provider-safe）  
   - `content` = 文本；可选前缀仅作 fixtures 兜底，须 golden 锁定  
3. Member 最终产出写入 Timeline **且** 作为 Leader assign 的 tool result 时：Leader 侧用 `assign_id` **去重**，不因 Timeline 再插一条重复 user  

### 5.3 并行 tool_calls

v1：模型返回 N 个 tool_calls → 必须收集 N 个结果再续写 assistant；或整轮拒绝并写入可恢复错误。禁止半配齐。

### 5.4 Golden fixtures（目录约定）

```text
docs/design/fixtures/workgroup-d05/
  projection/
    openai_member_sees_leader.json
    deepseek_leader_sees_member_final.json
    tool_name_not_overridden.json
  identity/
    reject_display_name_as_protocol_name.json
    reserved_name_spoof.json
```

（D0.5 收尾时补齐文件；本章先冻结规则与路径。）

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

### 7.2 幂等

| 阶段 | 断线后行为 |
|------|------------|
| 未发出 | 可发新 `command_id` 或重发同一草稿策略由 Manage 定；v1 重用同一 id 直到 accepted |
| 已发未 ack | **重发同一** `command_id`+hash |
| 已 accepted | **禁止**新 id 重做；只查询状态 |
| running 未知 | → `indeterminate`（非幂等类） |
| succeeded/failed | 返回终态 |

`side_effect_class=fs_read` 等只读类：策略上仍走同一状态机；实现可更快标失败，但 **不得**在 accepted 后换 id 重放写类工具。

### 7.3 Manage / Node 持久化边界

- Manage：Assign、RunHistory、outbox（待投递 command）  
- Node：journal（command_id → 状态/结果）  
- 任一侧在边界崩溃：按 §7.2 恢复；journal 丢失且可能有副作用 → `indeterminate`

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
| `duplicate_client_message` | |

---

## 8. WS 契约

### 8.1 会话

1. Node 用**独立凭据**连接（fail-closed；不继承 Manage 开放模式）  
2. `session.hello` → Manage 分配/确认 `connection_generation`  
3. 同 `node_id` 新连接成功 → 旧 generation **立即 fencing**  
4. `resume.offer(last_ack_seq…)` → gap-fill 从 outbox 重放  

### 8.2 背压

- 超出窗口未 ack：暂停非关键扇出或断开（实现选一种，fixtures 锁定）  
- human 入队：Leader 同步 `@` 期间不打断；结束后按 `seq` 喂入  

### 8.3 与事实源

断线丢的是帧，不是 Timeline；重连靠 `seq` 补洞。

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

## 12. 契约测试清单（用例名）

实现前 fixtures / 测试必须覆盖：

1. `projection_openai_legal_sequence`  
2. `projection_deepseek_assistant_name_fallback`  
3. `protocol_name_rejects_unicode_display`  
4. `reserved_name_spoof_rejected`  
5. `human_client_message_id_dedupe`  
6. `concurrent_human_total_order_by_seq`  
7. `provision_retry_same_id_ok`  
8. `provision_same_id_different_digest_conflict`  
9. `tool_cmd_resend_before_accept`  
10. `tool_cmd_no_reexec_after_accept`  
11. `non_idempotent_handler_executes_once`  
12. `indeterminate_on_journal_loss_after_accept`  
13. `archive_fencing_rejects_stale_epoch`  
14. `acl_revoke_stops_timeline_read`  
15. `hitl_double_resolve_cas`  
16. `hitl_execution_approval_wrong_node_denied`  
17. `hitl_exactly_once_tool_result_resume`  
18. `manage_restart_pending_assign`  
19. `node_restart_each_persist_boundary`  
20. `ws_gap_fill_and_dup_connection_fence`  
21. `tool_catalog_revision_drift`  
22. `member_not_in_local_agents_api`  
23. `member_local_messages_api_rejected`  
24. `timeline_excludes_raw_tool_payload`  
25. `legacy_placement_cannot_drive_member`  
26. `empty_allow_names_means_no_tools`（非回退 Node 默认）  
27. `node_policy_can_only_tighten`  
28. `assign_result_deduped_vs_timeline`  

---

## 13. 完成检查表（退出 D0.5）

- [ ] §2 字段无阻塞 TBD  
- [ ] §3 状态转换表与 fencing 无歧义  
- [ ] §4 矩阵与产品正文一致  
- [ ] §5 投影规则 + fixtures 目录至少各 2 个 golden 文件  
- [ ] §7 错误码表稳定  
- [ ] §9–§10 威胁与旧数据策略已评审  
- [ ] §12 用例可分配到 Go/Python 测试名  
- [ ] 产品正文 §13 将本文件标为 **D0.5 现行契约**  
- [ ] **未**合并未门禁的 Manage turn kernel / Worker 大改  

**退出后下一动作**：D0.9 停 Placement 入口 → D1 Manage 基座。

---

## 14. 修订记录

| 日期 | 说明 |
|------|------|
| 2026-07-30 | 首稿：承接工作组正文 §0–§13 与 GPT §16.5 清单 |
