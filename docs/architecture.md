# DAgents 架构

**基线**：v0.10.4（2026-08-25）。本文描述当前实现，不是历史方案或未来承诺；发生冲突时以代码、Schema 和 `CHANGELOG.md` 为准。

## 1. 系统边界

DAgents 由一个本地优先的 Agent Node 和一个可选的 Manage 控制面组成：

```text
浏览器 / API Client
        │ HTTP + SSE
        ▼
┌──────────────────────────── Agent Node（Go）
│  HTTP API / Web UI
│       │
│       ▼
│  session.Manager
│       └─ runtime（每个 Agent 会话独立）
│            ├─ MessageQueue + consumeLoop
│            ├─ turn.Orchestrator（LLM + tools + HITL）
│            ├─ Context/Skills/Compression
│            └─ SQLite / runtime side effects
│
│  Workgroup Dialer ───────────────┐  Node 主动发起
└─────────────────────────────────┼──────────────► Manage（Python）
                                  │ WS：注册、目录、工作组事件与控制
                                  ▼
                         Registry / Workgroup / Console / Releases
```

职责边界：

| 组件 | 负责 | 不负责 |
|---|---|---|
| Agent Node | Agent 会话、LLM turn、工具执行、审批、终端、SSE、Node Web UI | Manage 的全局目录与控制面存储 |
| session runtime | 单 Agent 的队列、历史、运行态、side-effect 对账 | 进程级 Agent 目录 |
| turn | 单个模型 Step、工具批处理、HITL 暂停/恢复、模型可见上下文 | 会话 CRUD、队列消费、持久化 |
| Manage | Node Registry、工作组 Leader、ACL、Timeline、RunHistory、Console | 直接访问 Node；不主动向 Node 发 HTTP |
| Web UI / Console | 展示权威事件、提交用户动作 | 自己推断运行态或绕过 Node/Manage API |

## 2. 一次本地请求

```text
POST /v1/messages
  → api 解析与鉴权
  → session.Manager.EnqueueMessage
  → runtime.queue（按 request type / priority 排序）
  → consumeLoop
  → turn.Orchestrator.runOneStep
  → LLM 流式响应
  → tool policy：auto / HITL / deny / background
  → tool result 入队或等待 resume
  → persist + SSE
```

同一 runtime 同时只有一个 consumer。一次 human message 可以跨多个模型 Step；每个 Step 都有独立生命周期事件，工具结果通过队列续跑，而不是在 HTTP handler 中递归调用下一轮模型。

队列中的主要来源是 `message`、`resume`、`tool_result`、`async_tool_result`、`trigger_message` 和 `side_effect_continue`。`tool_result` 优先闭合已经发出的工具批；用户消息仍可打断等待中的 HITL，具体规则见 [turn / session 包文档](subsystems/README.md)。

## 3. Session、Turn、Step

- **Agent**：Node 上可配置、可注册的长期对象，拥有模型、工具组、policy、skills 和记忆侧车。
- **Session**：一个 Agent 的独立对话历史、队列、runtime 和上下文边界；不同 session 不共享可变 history。
- **Turn**：由一次 human、resume、tool result 或 side effect 触发的连续执行单元；可能包含多个 Step，直到终态或 HITL 暂停。
- **Step**：一次模型请求及其工具批处理边界。模型请求、工具结果、usage 和终态都关联 Step。
- **Context snapshot**：一个 Step 使用的稳定输入视图。运行时信息通过 request-only `ContextInjection` 进入请求副本；不会把动态尾部追加到普通历史。

压缩在父 session 的 Step 边界处理；压缩请求复用 system/tools/history 前缀，尽量避免无必要的 Prompt Cache 破坏。Skills 正文属于显式加载的会话状态，变化在下一个允许的 Step 生效，不在正在进行的模型请求中途改写输入。

## 4. 工具与安全边界

模型看到的是 Registry 生成的工具 Schema；工具描述不重复拼进 system message。实际执行还要经过：

```text
tool schema → tool router → policy / hook → executor → result contract → history + SSE
```

工具结果通过结构化状态区分成功、失败、拒绝、运行中、取消、超时和未知；正文保留工具自身证据。文件工具使用工作区相对路径，Shell/终端是否可用由工具组、policy 和运行态共同决定。HITL 的审批事实由 Node 或 Manage 持久化，UI 只能依据权威事件和快照更新卡片。

## 5. Workgroup

工作组由 Manage 维护，成员引用 Node 上已经注册的 `agent_id`，不再创建一个与本地 Agent 脱节的受限副本。Node 是连接发起方：

```text
Node Dialer → Manage WS
  hello / resume / ack
  ← agent.session.open / turn.start / turn.cancel / session.close
  → ready / realtime / timeline / result / delivery ack
```

Manage Leader 负责工作组编排；成员 Agent 在自己的 Node 上执行工具。直达成员消息不投影成 Supervisor 任务。公开进度进入 Timeline，流式状态进入 `workgroup.realtime`；重连时以 Timeline 游标和持久化状态对账。

工作组成员会话按 `workgroup_id + member_id + agent_id` 隔离，成员只能使用其本地 Agent 的有效能力与 Node 侧收紧后的策略。完整协议和 JSON fixtures 见 [Workgroup 契约](design/workgroup-d05-contracts.md)。

## 6. 持久化与恢复

| 数据 | 位置/所有者 | 恢复方式 |
|---|---|---|
| Agent、对话、skills 状态 | Node SQLite / `.runtime` | Agent ensure 后 hydrate runtime |
| turn/step 生命周期 | Node session event / timeline projection | 游标读取与终态对账 |
| 工具审批 | Node 或 Manage HITL store | CAS resolve，重复决议幂等 |
| Workgroup、ACL、assign、Timeline | Manage SQLite | Node 通过 WS resume |
| 终端 | Node 内存会话及终端协议 | 页面离开只分离；权威关闭或超时才终止 |

重启、断线、迟到结果和重复控制帧必须经过 epoch/generation/cursor 等 fencing；不能用 UI 刷新或“最后一次请求返回”代替状态一致性。

## 7. 源码导航

| 主题 | 入口 |
|---|---|
| Node 进程装配 | `node/cmd/dagents-node/`、`node/internal/api/` |
| Agent/session runtime | `node/internal/session/` |
| Turn/Step 编排 | `node/internal/turn/` |
| 队列 | `node/internal/queue/` |
| 工具、policy、Hook | `node/internal/tools/`、`policy/`、`hooks/` |
| Context、skills、压缩 | `promptcontext/`、`skills/`、`compression/` |
| Node↔Manage / Workgroup | `node/internal/manage/`、`node/internal/workgroup/`、`manage/workgroup/` |
| Node Web UI | `node/webui/frontend/` |
| Manage Console | `manage/console/frontend/` |
| 共用契约 | `shared/config/`、`shared/workgroup/`、`docs/design/fixtures/` |

接口、配置、工具和事件的入口见 [reference/README.md](reference/README.md)；贡献与验证见 [development.md](development.md)。
