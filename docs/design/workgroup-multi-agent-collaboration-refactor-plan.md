# Workgroup 多智能体协作重构方案

状态：已落地基线。本文档保留为开发对照；实际协议以代码、`workgroup-and-node-gateway.md` 和 `node/internal/workgroup/REFERENCE.md` 为准。

本文档根据 Codex 的父子 Thread 协作模型、DeepSeek Harness 的 Session/Event/Inbox 模型，以及 DAgents 当前 Manage/Node/Workgroup 实现制定。目标不是复制任何一个项目，而是在保留 DAgents 跨机器能力的前提下，收敛多智能体委派、审批、恢复和前端投影的职责。

## 1. 背景与问题

当前 Workgroup 同时承担了五类职责：

1. Manage 侧 Leader 的 LLM 编排。
2. 成员任务的 Assign 创建、并发和等待。
3. Node 侧 Agent Session、Turn、工具和 HITL 的桥接。
4. Manage/Node 之间的 Outbox、WebSocket、重连和结果回放。
5. 前端根据 Timeline、实时事件和 HITL 状态推断任务展示。

当前已经具备正确的基础边界：Manage 管工作组和跨机器控制，Node 管 Agent 的实际执行；但存在以下结构性问题：

- `Assign`、`ActorRun`、Node `session_id`、Node `turn_id` 和 `tool_call_id` 的语义没有完全分离。
- 用户 `@成员` 与 Leader `assign_workgroup_task` 走两套执行和前端投影路径。
- Manage 的 `TurnKernel` 与 Node Agent Session 都参与调度，取消、超时和恢复需要跨层补偿。
- Manage 的等待者、结果缓存和 Node bridge 活跃映射主要是进程内状态，持久化状态与执行状态存在间隙。
- 工具审批和 Ask User 都放在 HITL 范畴内，但触发者、权限语义和恢复对象不同。
- 前端直接消费原始工具事件，导致同一 Assign 或 HITL 可能被渲染成多个卡片。
- 当前成员 Turn 通过 Assign 等待最终结果，无法表达清晰的 foreground/background 两种交付语义。

本方案优先解决“一个委派任务只有一个权威生命周期”和“一个用户交互只有一个恢复目标”两个问题。

## 2. 参考项目提炼出的原则

### 2.1 Codex 的可借鉴部分

Codex 将根 Agent 与子 Agent 放在独立的 Thread/Turn 上，通过显式协作操作连接：

```text
根 Thread
  └─ 根 Turn
       ├─ spawn_agent
       ├─ send_input
       ├─ wait
       └─ interrupt
            ↓
         子 Thread / 子 Turn
```

可借鉴原则：

- 子 Agent 拥有独立上下文，父 Agent 不接收所有子 Agent 原始消息。
- 父子通信使用显式的协作操作，不使用伪造的用户消息。
- 子 Agent 的活动、结果和 Turn 完成是不同事件。
- 子任务使用稳定 ID，可独立查看、取消和恢复。
- 父 Agent只接收子任务结果或必要的状态摘要。
- 并发上限是协议的一部分，而不是散落在工具实现中。

不直接照搬：

- 不把完整 Leader 历史默认复制给成员 Agent。
- 不把 DAgents 的跨 Node 成员建模成普通本地子线程。
- 不把所有 Codex 协作工具一次性加入 DAgents。

参考：

- [OpenAI Multi-agent guide](https://developers.openai.com/api/docs/guides/responses-multi-agent)
- [Codex app-server README](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
- [Codex multi-agent spawn spec](https://github.com/openai/codex/blob/main/codex-rs/core/src/tools/handlers/multi_agents_spec.rs)

### 2.2 DeepSeek Harness 的可借鉴部分

DeepSeek Harness 将 Agent Loop 与 Subagent 能力分离：Agent Loop 只负责模型调用、工具执行、结果落盘和下一步；Subagent 作为可选能力通过 Provider、Inbox 和 Report 接入。

可借鉴原则：

- Session Event Log 是事实来源，内存中的 Activation/Waiter 只是运行时加速结构。
- One-shot 与 Continuable 是两种明确模式，不把一次性任务和长期子 Agent 混成一个抽象。
- 父子消息有明确权限，子 Agent不能任意访问兄弟或祖先。
- `interrupt` 只表示中断请求已被接纳，不假设业务已经立即停止。
- 后台任务通过稳定任务 ID 和显式报告投递，不插入伪造 human message。
- Report 可以选择唤醒父 Agent，或者只记录结果等待后续读取。
- 取消后，未派发的工具调用也要补齐失败结果，保持模型消息序列合法。

参考：

- [DeepSeek Harness core](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/core.md)
- [DeepSeek Harness agent-loop](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/agent-loop/README.md)
- [DeepSeek Harness subagent](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/subagent.md)
- [DeepSeek Harness tool-subagent](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/subagent/tool-subagent/src/index.ts)

## 3. 目标与非目标

### 3.1 目标

- 保留 Manage/Node 的跨机器双层架构。
- 让现有 `Assign` 成为唯一的子任务委派实体。
- 统一 direct member 与 Leader tool 两条入口。
- 明确 Session、Turn、Assign、Attempt、Tool Call 的 ID 和生命周期。
- 让 Node 成为成员 Agent 实际 Turn、工具和工具审批的唯一执行权威。
- 让 Manage 成为工作组控制面、可靠投递层和用户交互代理。
- 将工具审批和 Ask User 分成不同语义、不同恢复路径。
- 支持 foreground，并为后续 background 留出明确协议。
- 让前端按 Assign/HITL 聚合，而不是按原始事件猜测任务。
- 在取消、重连、重启、迟到事件和重复投递下保持状态单向推进。

### 3.2 非目标

- 不把 Workgroup 改造成通用 A2A 平台。
- 不取消 Manage/Node 的分布式边界。
- 不在第一阶段引入独立的 `Delegation`、`SubagentTask`、`ChildTask` 多套重复实体。
- 不默认把完整父上下文传给子 Agent。
- 不让 Manage 直接执行成员工具。
- 不在第一阶段实现任意成员之间的自由通信。
- 不通过“插入一条 human message”解决后台回调、审批或子任务恢复。
- 不改变 Tauri 双轨策略；本文只涉及 Workgroup 多智能体模块。

## 4. 目标架构

```text
┌─────────────────────────────────────────────┐
│ Manage：Workgroup Control Plane              │
│                                             │
│ Workgroup / Member / ACL                    │
│ Parent Turn / Assign / admission            │
│ HITL durable projection / Outbox            │
│ Parent result delivery / recovery           │
└──────────────────────┬──────────────────────┘
                       │ Outbox + WebSocket
                       │
┌──────────────────────▼──────────────────────┐
│ Node：Agent Execution Plane                  │
│                                             │
│ AgentRef Session / Child Turn                │
│ LLM / Tool Registry / Tool Policy            │
│ Tool Approval / Tool Cancel                  │
│ Private History / Turn Resume               │
└──────────────────────┬──────────────────────┘
                       │ typed child events
                       │
┌──────────────────────▼──────────────────────┐
│ Projection：Timeline + UI                    │
│                                             │
│ AssignViewModel                              │
│ 一个 assign_id 一个任务卡片                  │
│ 一个 hitl_id 一个交互卡片                    │
└─────────────────────────────────────────────┘
```

### 4.1 Manage 的职责

Manage 负责：

- 工作组和成员关系。
- 成员 AgentRef、ACL 和跨 Node 路由。
- 根用户输入与 Leader Turn 的排队。
- Assign 的创建、准入、状态和父子关系。
- foreground/background 交付策略。
- HITL 的持久化投影、用户权限和可靠投递。
- Node 连接、Outbox、重连和事件接收。
- 向父 Leader 或前端推送必要的状态和结果。

Manage 不负责：

- 执行成员 Agent 的 LLM Loop。
- 判断成员工具是否需要审批。
- 直接执行成员工具。
- 根据一段文本推断 Node 工具状态。
- 伪造成员 Agent 的 tool result。

### 4.2 Node 的职责

Node 负责：

- 成员 Agent Session 和私有历史。
- 成员 Agent Turn 的开始、排队、取消和恢复。
- 成员 Agent 的模型调用和工具循环。
- 工具策略、工具审批和工具执行。
- 工具级取消。
- 生成合法的 assistant tool_call/tool_result 配对。
- 发送结构化执行事件和最终结果。

Node 不负责：

- 工作组 Leader 的用户消息队列。
- 工作组成员 ACL。
- 前端工作组 Timeline 的最终布局。
- 代替父 Agent决定是否继续分派其他成员。

### 4.3 前端的职责

前端只消费 Manage 提供的统一投影：

- 一个 Assign 渲染一个任务卡片。
- 一个 HITL 渲染一个审批或询问卡片。
- 工具事件更新任务卡片内部进度。
- 通过稳定 ID 幂等合并重复事件。
- 不根据事件到达顺序推断最终业务状态。

## 5. 核心领域模型

### 5.1 不新增平行委派实体

现有 `Assign` 已经承担工作组成员委派关系，建议将它提升为唯一的 Assignment/Delegation 概念。数据库表 `workgroup_assigns`、API 字段 `assign_id` 暂时保留，避免一次重构同时引入命名迁移和行为迁移。

不要同时维护以下多个对象：

```text
Assign
Delegation
SubagentTask
ChildTask
MemberJob
```

如果未来确实需要对外改名，只能通过 API 兼容别名完成，内部仍保持一个权威记录。

### 5.2 Assign 字段建议

现有字段保留，并补充或明确以下字段：

```text
assign_id              # 委派关系的稳定 ID
workgroup_id
parent_run_id          # 父 Agent 持久运行上下文
parent_turn_id         # 父 Agent 发起委派的 Turn
parent_tool_call_id    # Leader tool 路径可有，direct 路径为空

source                 # direct_member | leader_tool
delivery_mode          # foreground | background

member_id
agent_id
home_node_id
member_session_id      # Node Agent Session
child_turn_id          # Node 当前 Turn，不能再复用 assign_id
attempt_id             # 一次执行尝试，重试时变化

status                 # queued | running | awaiting_hitl |
                       # succeeded | failed | canceled | indeterminate
state_version          # CAS 和迟到事件防护
last_event_seq         # 子 Turn 事件消费进度

result_summary
result_message_id
error_code
created_at
updated_at
```

实现时不必一次把所有字段暴露给前端，但必须在后端建立明确语义。尤其要禁止以下替代关系：

```text
assign_id != child_turn_id
parent_run_id != parent_turn_id
member_session_id != child_turn_id
tool_call_id != assign_id
```

### 5.3 ActorRun 的语义

当前 `ActorRun` 在部分路径中承担持久 Agent 上下文和 RunHistory 容器的作用，并不等价于一次 LLM Turn。后续应将其语义固定为“Actor Session/Run Context”，不再用它表示 Node 的一次 child turn。

第一阶段可以保留类名和表名，先通过字段和文档澄清；第二阶段再评估是否引入内部别名 `ActorSession`。不应在没有迁移收益时进行纯命名重构。

### 5.4 HITL 统一载体、分离语义

继续使用当前 HITL 持久化表，但必须明确 `kind`：

```text
kind = tool_approval
kind = user_question
```

建议通用字段：

```text
hitl_id
workgroup_id
kind
owner                 # leader | child
assign_id
parent_run_id
parent_turn_id
child_session_id
child_turn_id
node_hitl_id
tool_call_id
status
resolution
created_at
resolved_at
delivery_status
```

`Manage HITL status=resolved` 只代表 Manage 已经记录用户决定，不能代表 Node 已经恢复或工具已经成功执行。必要时增加单独的 `delivery_status`：

```text
not_required
pending_delivery
delivered
applied
delivery_failed
```

### 5.5 子 Turn 事件

Node → Manage 的 `agent.turn.event` 必须具备业务级顺序和幂等信息：

```json
{
  "event_id": "evt_xxx",
  "event_seq": 17,
  "workgroup_id": "wg_xxx",
  "assign_id": "as_xxx",
  "member_id": "mb_xxx",
  "agent_id": "agt_xxx",
  "session_id": "ses_xxx",
  "child_turn_id": "turn_xxx",
  "attempt_id": "attempt_xxx",
  "event_type": "tool_started",
  "occurred_at": "2026-09-03T12:00:00Z",
  "data": {}
}
```

建议事件类型：

```text
session_ready
turn_accepted
turn_state
assistant_delta
reasoning_delta
tool_started
tool_approval_required
tool_finished
user_question_required
turn_awaiting_hitl
turn_resumed
turn_result
turn_canceled
turn_failed
```

`event_seq` 只在一个 `assign_id + child_turn_id + attempt_id` 范围内递增。WebSocket 的 `delivery_seq` 解决传输顺序，不能替代业务事件序号。

## 6. Assign 生命周期

### 6.1 外部状态

为保持 API 兼容，外部 Assign 状态优先保留当前集合：

```text
queued
running
awaiting_hitl
succeeded
failed
canceled
indeterminate
```

更细的 `accepted`、`dispatching`、`resuming` 通过事件或内部 execution phase 表达，避免为了展示阶段无限增加顶层状态。

### 6.2 标准流程

```text
queued
  ↓ admission accepted
running
  ├─ tool approval required → awaiting_hitl
  │                              ↓ resume accepted
  │                           running
  ├─ user question required → awaiting_hitl
  │                              ↓ answer accepted
  │                           running
  └─ terminal result
       ├─ succeeded
       ├─ failed
       ├─ canceled
       └─ indeterminate
```

### 6.3 状态不变量

- Assign 进入终态后不能被迟到事件重新激活。
- `canceled` 优先于迟到的成功结果；成功结果只能作为审计事件保存，不能改变 Assign 状态。
- 同一成员最多有一个 active Assign。
- 同一 Assign 的同一 `attempt_id` 只能有一个终态结果。
- `state_version` 必须单向递增，跨线程或重连更新使用 CAS。
- 任何 `awaiting_hitl` 必须能通过 `hitl_id` 找到唯一恢复目标。
- 任何 Node `tool_call` 最终必须有且只有一个对应 `tool_result`。
- Node 断线不能直接推断成功；无法确认执行结果时进入 `indeterminate`。

## 7. 两种委派入口统一

### 7.1 统一入口

新增内部服务接口，名称可按现有代码风格调整：

```python
request_assignment(
    workgroup_id,
    *,
    source,
    member_id,
    instruction,
    parent_run_id,
    parent_turn_id=None,
    parent_tool_call_id=None,
    delivery_mode="foreground",
) -> Assign
```

它统一完成：

1. 校验成员和 ACL。
2. 检查同一成员单飞。
3. 创建 Assign 并持久化。
4. 占用成员。
5. 写入统一的 `assign_created`/`assign_started` Timeline。
6. 发送 Node `agent.turn.start`。
7. 注册结果和事件消费关系。

### 7.2 Leader tool 路径

```text
Leader LLM
  → assign_workgroup_task
  → request_assignment(source=leader_tool)
  → Node child turn
  → result
  → tool result 回 Leader
  → Leader 继续下一步
```

Leader tool 调用的工具结果必须绑定原始 `parent_tool_call_id`，不可由 Assign 完成事件替代。

### 7.3 Direct member 路径

```text
用户 @成员
  → request_assignment(source=direct_member)
  → Node child turn
  → result
  → 直接回用户
```

direct 路径只跳过 Leader LLM，不跳过 Assign、Node Session、事件、HITL、取消和结果投影。

不再保留独立的 `_run_direct_member_events` 业务生命周期。可以保留 direct 的入口适配器，但它只能组装参数并调用统一服务。

### 7.4 父上下文中的 direct 结果

direct 路径没有真实的 Leader tool_call，不能简单把成员结果伪装成普通用户消息。建议增加内部 `assignment_result` 历史项：

```json
{
  "kind": "assignment_result",
  "assign_id": "as_xxx",
  "member_id": "mb_xxx",
  "status": "succeeded",
  "summary": "...",
  "timeline_event_seq": 42
}
```

LLM 适配器在下一次 Leader Turn 中将其序列化为稳定的上下文块；它是系统产生的成员结果，不是新的用户意图。第一阶段可暂时兼容现有历史格式，但新代码不得继续扩大“成员结果作为 user role”的使用范围。

## 8. 工具审批设计

### 8.1 所有权

工具审批由 Node 负责业务判断和执行恢复：

```text
Node Tool Policy
  → 判断需要审批
  → 创建 Node pending HITL
  → 暂停 child turn
```

Manage 只是：

- 保存用户可见的 HITL 代理记录。
- 进行工作组级权限检查。
- 向前端推送。
- 接收用户决定。
- 通过 Outbox 将决定可靠发送给 Node。

Manage 不能本地宣布工具已批准，也不能直接执行工具。

### 8.2 批量审批

一次模型步骤中多个需要审批的工具调用，可以聚合为一个 `hitl_id`，每个工具为一个 item：

```json
{
  "hitl_id": "hitl_xxx",
  "kind": "tool_approval",
  "owner": "child",
  "assign_id": "as_xxx",
  "child_turn_id": "turn_xxx",
  "items": [
    {
      "item_id": "item_1",
      "tool_call_id": "call_1",
      "tool_name": "bash",
      "purpose": "安装依赖",
      "preview": "npm install"
    },
    {
      "item_id": "item_2",
      "tool_call_id": "call_2",
      "tool_name": "write_file",
      "purpose": "更新配置",
      "preview": "config.yaml"
    }
  ]
}
```

默认 UI 使用“全部允许/全部拒绝”。后续如果支持单项决定，resolution 必须明确每个 item 的结果。

### 8.3 审批恢复

```text
assistant(tool_call)
  ↓
Node tool_approval_required
  ↓
child turn = awaiting_hitl
  ↓
user approve/deny
  ↓
Node agent.turn.resume(hitl_id, resolution)
  ↓
tool executes or is denied
  ↓
tool(result)
  ↓
child Agent continues
```

审批恢复不是新 Turn，也不是新的 human message。

结果必须满足：

```text
assistant(tool_call: call_1)
tool(tool_result: call_1)
```

拒绝、取消、过期也必须生成失败型 `tool_result`。

### 8.4 审批取消和过期

- 用户取消 Assign：取消关联 child Turn，并将 pending approval 标记 canceled。
- 用户取消整个 Workgroup Turn：只取消当前父 Turn 及其 foreground 子任务。
- Node 断线：Manage 保留 `pending_delivery`，由 Outbox 重试。
- HITL 已过期：Node 生成 `expired` 的 tool result，不再执行工具。
- 旧卡片点击：通过 HITL CAS 失败，不能重新激活任务。

## 9. Ask User Information 设计

### 9.1 Leader 询问用户

现有 `ask_workgroup_user` 的方向保留：

```text
Leader assistant(tool_call: ask_workgroup_user)
  ↓
Manage 创建 kind=user_question
  ↓
Leader Turn awaiting_hitl
  ↓
用户回答
  ↓
写入原 tool_call 的 tool_result
  ↓
Leader 继续原 Turn
```

消息序列必须是：

```text
assistant(tool_call: ask_workgroup_user)
tool(tool_result: answer)
assistant(...)
```

不能插入：

```text
user(answer)
```

当前实现中的 `native_tools._ask_user()`、`resume_resolved_hitl()` 可以作为兼容实现，但应将 HITL 类型由泛化的 information 明确为 `user_question`。

### 9.2 子 Agent 询问用户

子 Agent 的问题由 Node Session 所有：

```text
Member Agent
  → Node 创建 child user_question
  → child turn awaiting_hitl
  → Node → Manage
  → Manage → UI
  → 用户回答
  → Manage → Node agent.turn.resume
  → Member Agent 继续
```

父 Agent默认只收到：

```text
Assign.awaiting_hitl(kind=user_question)
```

父 Agent不直接消费子 Agent的问题答案。

### 9.3 需要父 Agent决策的情况

如果问题不是让最终用户回答，而是需要父 Agent决策，应使用显式父子消息：

```text
child → parent: request_decision
parent → child: send_input
```

这不是工具审批，也不是普通 human message。它应该绑定：

```text
assign_id
parent_turn_id
child_turn_id
message_id
```

并限制为 direct parent/child 通信。

## 10. 父 Agent 的交付模式

### 10.1 Foreground：第一阶段默认模式

```text
Leader assign_workgroup_task
  → 当前 Leader tool step 暂停
  → 子 Agent执行
  → 子 Agent可能等待审批/询问
  → 子 Agent返回最终结果
  → 生成 tool_result
  → Leader继续
```

优点：

- 与当前行为兼容。
- 父上下文容易保持合法。
- 子任务结果天然绑定原始工具调用。
- 不需要第一阶段引入父 Agent后台消息投递。

### 10.2 Background：第二阶段可选模式

只有调用方显式指定 `delivery_mode=background` 时才启用：

```text
Leader assign(background)
  → 返回 accepted + assign_id
  → Leader继续其他任务
  → 子 Agent独立执行
  → 完成后生成 assignment_result
```

后台结果交付策略：

```text
quiet  # 持久化结果，但不主动唤醒 Leader
wakeup # 将结果排入父 Agent下一次可执行 Turn
```

后台结果不能通过伪造 human message 进入父上下文。应使用内部 parent inbox/context injection，并保证：

- 每个 `result_message_id` 只投递一次。
- 结果到达顺序由 `created_at + event_seq` 确定。
- 父 Turn取消后，wakeup 不得复活已取消的父 Turn。
- 进度事件默认只推送 UI，不持续注入父模型，避免 Token 爆炸。

## 11. 调度与并发

### 11.1 调度权归属

```text
Manage TurnKernel
  只调度：工作组根输入、Leader Turn、Assign 准入

Node Agent Session
  只调度：成员 child Turn、工具调用、工具审批恢复
```

Manage 可以保留根工作组 human queue，但不能再次实现一套成员 Agent Turn 队列。

### 11.2 并发规则

- 工作组级别有最大 active Assign 数。
- 同一成员单飞。
- 不同成员可并行。
- 同一 child Turn 内的工具是否并行，由 Node 工具策略决定。
- Leader 一批多个独立 Assign 可以并行，但并行上限必须集中配置。
- foreground Assign 占用父 Leader 当前工具轮；background Assign 不占用父 Turn执行槽。

建议保留当前的工作组并发上限，但将它从 `TurnKernel` 内的具体线程池实现提升为 Workgroup Scheduler 配置。线程池只是实现细节。

### 11.3 背压

当达到并发上限时：

```text
Assign = queued
member.active_assign_id 不变
前端显示排队位置
```

不得创建 Assign 后再依赖内存线程决定是否真正执行。队列记录必须在持久化后再启动 worker。

## 12. 取消、恢复与消息序列

### 12.1 取消层级

```text
tool cancel
  取消单个工具，child turn根据 tool_result继续或结束

child turn cancel
  取消一个成员 Agent Turn

assign cancel
  取消一次成员委派及其 child turn

parent turn cancel
  取消 Leader Turn，并按 delivery_mode处理关联 Assign

workgroup cancel
  取消当前根 Turn及其可取消子任务
```

每一层取消必须有明确的 `cancel_id`、目标 ID 和最终确认状态。

### 12.2 中断不是立即完成

取消请求采用以下语义：

```text
cancel accepted ≠ process stopped ≠ result reconciled
```

Node 返回 `cancel accepted` 后，仍可能有迟到的工具结果或流式片段。Manage 必须依据 Assign/Turn 的 CAS 终态过滤迟到事件。

### 12.3 取消后的消息修复

对于模型已经生成但尚未执行的工具调用：

```text
assistant(tool_calls=[call_1, call_2])
```

如果只执行了 `call_1`，则必须补齐：

```text
tool(call_1, result=...)
tool(call_2, result={"status":"aborted_before_dispatch"})
```

不能让模型历史停留在未配对的 assistant tool_call 上。

对于正在流式生成的 assistant：

- 流式草稿只能进入实时 UI。
- 只有完成或中断收口后才写入 Canonical History。
- 中断时保存 delivered prefix，并带 `interrupted=true`。
- 下一次 Turn 不能继续使用未收口的临时消息。

相关现行边界见：[turn-cancellation-message-integrity-plan.md](turn-cancellation-message-integrity-plan.md)。

## 13. 重连与重启恢复

### 13.1 Outbox

继续使用现有 Outbox：

```text
持久化 command/outbox
  → 发送 Node
  → Node command_id/payload_hash 去重
  → ACK
```

`agent.turn.resume` 必须具备稳定的 `command_id`，同一个用户决定不能因为重连被恢复两次。

### 13.2 事件消费

Manage 为每个 Assign 保存 `last_event_seq` 或等价事件游标：

- 重复事件：忽略或返回已处理。
- 跳号事件：请求 Node 重新发送缺失事件，或请求当前 Assign 快照。
- 迟到终态：写审计日志，不改变已完成 Assign。
- 连接代次变化：旧连接事件不能覆盖新状态。

### 13.3 Manage 重启

重启后按以下顺序恢复：

1. 读取持久化 Assign、HITL、Outbox 和事件游标。
2. 重新建立 Node WS。
3. 对 `queued` Assign 继续派发。
4. 对 `running/awaiting_hitl` Assign 请求 Node 状态或事件回放。
5. 有明确 child result 的，幂等收口为终态。
6. 无法确认远端执行状态的，进入 `indeterminate`。
7. pending HITL 保留给用户，不因 Manage 重启消失。

当前 `reconcile_inflight_runs()` 将部分活跃记录标记为 indeterminate 是安全兜底，但目标是先通过 Node 状态查询和事件回放减少不必要的 indeterminate。

### 13.4 Node 重启

Node 重启后：

- Agent Session 私有历史仍是 Node 事实来源。
- 未完成 child Turn 必须通过 session/attempt 状态恢复或明确标记未知。
- pending Node HITL 不能因为内存 map 丢失而失效。
- Manage 的 HITL 代理与 Node `node_hitl_id` 必须可重新关联。

如果短期内不能持久化 Node bridge 的全部运行时映射，至少要持久化：

```text
session_id
child_turn_id
attempt_id
assign_id
node_hitl_id
last_event_seq
command_id
```

## 14. 前端投影重构

### 14.1 统一 ViewModel

前端应建立单一 Assignment reducer，输入来源可以包括：

- 初始 Assign 快照。
- Timeline 快照。
- HITL 快照。
- 实时 child event。
- Assign result。

输出结构建议：

```js
{
  assignId,
  source,
  member: {
    memberId,
    displayName,
    agentId
  },
  instruction,
  status,
  executionPhase,
  currentTool,
  completedTools,
  approval,
  question,
  result,
  error,
  lastEventSeq
}
```

### 14.2 渲染规则

```text
assign_id  → 一个任务卡片
hitl_id    → 一个审批/询问卡片
tool_call_id → 任务卡片内部一个工具详情
event_id   → 幂等更新标识
```

Direct 和 Leader assign 使用同一任务卡片组件，只通过 `source` 显示来源差异。

### 14.3 审批和询问卡片

`tool_approval`：

- 显示工具名、风险摘要、关键参数和目的。
- 批量 item 在一个卡片中展示。
- 默认提供全部允许/全部拒绝。
- 不展示父 Agent 的内部 tool_call 细节。

`user_question`：

- 显示问题正文和可选项。
- 提供输入控件和提交状态。
- 不显示“批准/拒绝”措辞。
- 回答后显示“已提交，正在继续”。

### 14.4 刷新重建

刷新后必须能够只依赖：

```text
Assign snapshot
Timeline snapshot
HITL snapshot
last_event_seq
```

重建出与实时状态等价的任务卡片。不能依赖前端内存中曾经收到过某个 `tool_started` 事件。

## 15. Token 与上下文策略

- 子 Agent默认接收自包含任务指令，而不是完整 Leader RunHistory。
- 只传递必要的工作组、成员、工作区和约束元数据。
- child progress 默认只推送 UI，不注入父模型。
- foreground 只把最终结果作为 Leader tool result返回。
- background 只通过显式 report/wakeup 注入紧凑结果。
- 工具审批参数保留在子 Agent历史和 Manage审计中，父 Agent只获得状态摘要。
- 子 Agent的完整私有历史不进入公共 Timeline。
- 父 Agent收到的结果应限制长度，并保留结构化状态、错误码和摘要。

目标是将 Token 开销集中在真正需要模型推理的地方，而不是把每个子工具进度转成父 Agent上下文。

## 16. 代码落点与改造顺序

### Phase 0：协议和测试基线

涉及：

- `manage/workgroup/models.py`
- `manage/workgroup/store.py`
- `node/internal/workgroup/types.go`
- `node/internal/api/workgroup_agent_bridge.go`
- Workgroup 相关测试

工作：

- 定义 ID 关系和状态不变量。
- 增加事件 fixture 和状态转移测试。
- 明确 `tool_approval` 与 `user_question`。
- 不改变现有执行行为。

验收：

- 所有 Assign 状态转移有测试。
- 重复、迟到、跳号事件有测试。
- tool_call/tool_result 配对有测试。

### Phase 1：统一 Assign 创建和收口

涉及：

- `manage/workgroup/native_tools.py`
- `manage/workgroup/turn_kernel.py`
- `manage/workgroup/vertical.py`
- `manage/workgroup/store.py`

工作：

- 新增统一 `request_assignment()`。
- direct 和 Leader tool 都通过统一入口创建 Assign。
- 移除固定 `call_direct_1`。
- 将 `assign_id` 与 `child_turn_id` 分离。
- 统一 Assign 完成、失败、取消和成员释放。

验收：

- direct/Leader 两种入口生成同构 Assign。
- 两种入口都可以触发相同的审批、取消和恢复流程。
- 一个成员不能同时拥有两个 active Assign。

### Phase 2：Node 事件和恢复协议

涉及：

- `node/internal/workgroup/types.go`
- `node/internal/api/workgroup_agent_bridge.go`
- `node/internal/workgroup/dispatch.go`
- `manage/workgroup/vertical.py`
- `manage/workgroup/store.py`

工作：

- 增加 `event_id/event_seq/child_turn_id/attempt_id`。
- Manage 按事件序号幂等消费。
- `agent.turn.resume` 统一携带 Node HITL 路由信息和 command_id。
- 增加状态查询或事件回放能力。
- 将进程内 waiter 降级为性能优化，不作为最终事实来源。

验收：

- Manage/Node 重启后不会重复执行同一个 resume。
- 迟到成功事件不能复活 canceled Assign。
- 网络重连后能恢复 pending HITL 和未确认 Outbox。

### Phase 3：HITL 语义分离

涉及：

- `manage/workgroup/d3_models.py`
- `manage/workgroup/store.py`
- `manage/workgroup/vertical.py`
- `manage/workgroup/native_tools.py`
- Node `hitl`、`turn`、tool policy 相关模块

工作：

- `HITLRequest.kind` 分为 `tool_approval/user_question`。
- 工具审批只能由 Node 决定和恢复。
- Leader Ask User 继续恢复原 tool_call。
- Child Ask User 通过 Node child turn resume。
- 取消和过期生成合法 tool result。

验收：

- 工具审批不会创建父 Agent新 Turn。
- Ask User回答不会作为普通 human message插入。
- 一个批量审批只显示一个卡片。
- 旧 HITL 响应不能重新启动任务。

### Phase 4：前端统一投影

涉及：

- `node/webui/frontend/src/composables/useWorkgroupTimeline.js`
- `node/webui/frontend/src/components/WorkgroupToolRow.vue`
- `node/webui/frontend/src/components/WorkgroupApprovalCard.vue`
- `node/webui/frontend/src/views/WorkgroupView.vue`

工作：

- 引入 Assignment reducer。
- 按 `assign_id` 聚合任务。
- 按 `hitl_id` 聚合审批和询问。
- 直接成员和 Leader assign 使用同一组件。
- 从快照重建 UI，不依赖历史实时事件。

验收：

- 刷新前后卡片数量和状态一致。
- 同一个审批不会出现多个卡片。
- 工具进度、当前工具、最终结果在同一任务卡片内稳定更新。

### Phase 5：可选 background

涉及：

- `assign_workgroup_task` schema
- Parent delivery/inbox
- `TurnKernel` parent continuation
- UI 后台任务状态

工作：

- 增加显式 `delivery_mode=background`。
- 支持 `quiet/wakeup`。
- 增加结果去重和父 Turn fencing。
- 不改变 foreground 默认行为。

验收：

- 后台任务不阻塞父 Agent继续执行。
- 子 Agent审批只暂停子 Agent。
- 最终结果只投递一次。
- 父 Turn取消后后台结果不会复活父 Turn。

## 17. 测试矩阵

### 17.1 协议与状态

- Assign 状态单向推进。
- 同一成员单飞。
- 不同成员并行。
- 重复 start/resume/cancel 幂等。
- 迟到 result 不复活已取消任务。
- 同一事件重复到达不重复写 Timeline。
- event_seq 跳号能发现并恢复。

### 17.2 工具审批

- 单工具批准。
- 单工具拒绝。
- 多工具全部批准。
- 多工具全部拒绝。
- 单项批准/拒绝（如果启用）。
- 审批等待期间刷新。
- 审批等待期间 Manage 重启。
- 审批决定落盘但 Node 尚未收到时断线。
- 审批后用户立即取消 Assign。
- 取消后迟到的 tool result。

### 17.3 Ask User

- Leader 询问并回答。
- Leader 询问后取消 Turn。
- Leader 询问后 Manage 重启。
- 子 Agent询问并回答。
- 子 Agent询问期间取消 child Turn。
- 子 Agent问题升级给父 Agent。
- 回答不会产生未配对的 tool_call。

### 17.4 两种委派入口

- direct 与 Leader tool 生成相同 Assign 字段。
- direct 成员任务成功、失败、取消。
- Leader foreground 成功、失败、取消。
- 多成员并行。
- 同成员第二个任务排队或冲突。
- direct 和 Leader 路径都能触发工具审批。

### 17.5 重启和网络

- Node 断线后 Outbox 重发。
- Manage 断线后事件游标恢复。
- Manage 重启后 pending HITL 保留。
- Node 重启后 child session 可恢复或进入 indeterminate。
- 旧 connection generation 的事件被丢弃。
- 重复 resume 不产生两次模型继续调用。

### 17.6 前端

- 一个 Assign 只有一个任务卡片。
- 一个 HITL 只有一个审批/询问卡片。
- Timeline 快照和实时事件合并一致。
- 刷新后任务进度不丢失。
- 前端收到重复事件不重复展示。
- 工具结果、审批和询问的视觉语义不混淆。

## 18. 观测指标

建议增加以下指标，防止重构后只看“是否成功”而看不出复杂度变化：

```text
workgroup.assignment.created_total
workgroup.assignment.duration_seconds
workgroup.assignment.awaiting_hitl_seconds
workgroup.assignment.indeterminate_total
workgroup.assignment.duplicate_event_total
workgroup.assignment.event_gap_total
workgroup.assignment.resume_retry_total
workgroup.assignment.result_delivery_duplicate_total
workgroup.hitl.pending_total{kind,owner}
workgroup.hitl.resume_latency_seconds{kind,owner}
workgroup.child.active_total
workgroup.child.max_concurrency_rejected_total
workgroup.parent.context_injection_tokens
workgroup.child.result_tokens
```

重点观察：

- 同一 Assign 的事件数。
- Manage/Node 之间的重复投递数。
- HITL 从用户决定到子 Agent恢复的延迟。
- 重启后 indeterminate 比例。
- 父 Agent因子任务产生的额外 Token。
- direct 和 Leader 两条路径的失败率差异。

## 19. 迁移与兼容策略

### 19.1 数据兼容

- 保留 `assign_id`、`member_id`、`leader_run_id` 等旧字段。
- 新字段允许为空，旧 Assign 读取时从现有 metadata 推导。
- 旧 `HITLRequest.kind=information` 读取时按绑定字段兼容映射：
  - 有 Node tool call/approval items → `tool_approval`。
  - 有 Leader run/tool call 且是问答 → `user_question`。
- 不删除旧 Timeline 事件，新增统一投影时兼容读取。

### 19.2 协议兼容

- Node/Manage 协议增加字段时保持旧字段可选。
- `child_turn_id` 缺失时只能兼容旧 Assign，不允许新任务继续缺失。
- 新事件消费者必须忽略未知事件类型，但不能忽略未知终态。
- 旧 Node 不支持事件游标时，Manage 使用快照/终态查询兜底。

### 19.3 删除旧逻辑的条件

以下条件全部满足后，才能删除 direct 独立生命周期和旧 HITL 分支：

- direct/Leader contract tests 全部通过。
- 至少完成一次 Node/Manage 重启恢复测试。
- 审批、询问、取消、迟到事件测试覆盖。
- 前端刷新重建与实时展示一致。
- 生产或真实环境观察期内没有重复 Assign、重复 resume 和序列 400。

## 20. 最终决策摘要

最终结构应当是：

```text
Assign 是唯一委派实体
Manage 是控制面和可靠投递层
Node 是成员 Agent 执行权威
HITL 是统一存储载体，但 tool_approval/user_question 分离
父 Agent 接收状态和结果，不接收子工具原始审批
子 Agent自己暂停和恢复工具/询问
direct 与 Leader tool 使用同一 Assign 生命周期
foreground 默认保留，background 显式扩展
前端按 Assign/HITL 聚合，不按原始事件猜语义
```

最优先的开发顺序不是新增更多 Agent 协作工具，而是先统一身份、生命周期和投影。只有这些基础稳定后，才增加 background、父子消息和更多 continuable 能力。
