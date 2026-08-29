# Agent 实例模型

> **状态**：现行设计说明（v0.10.4）。本文只描述当前 Node/Agent/Session 边界；旧的 Session 中心、Placement 和沙箱设计见 [`docs/archive/`](../archive/README.md)。

## 1. 三个身份

| 身份 | 作用 | 生命周期 |
|---|---|---|
| `node_id` | 一个 Node 进程/主机的稳定身份 | Node 安装/运行时 |
| `agent_id` | Node 中一个真实、可持久化的 Agent | 用户创建/归档 |
| `session_id` | Agent 的一条独立消息与执行上下文 | 对话或工作组绑定 |

`agent_id` 是 Agent 目录主键，不能拿来替代 Session。默认个人对话通常是 Agent 的主 Session；加入 Workgroup 时，Manage 为该 `workgroup_id + member_id` 分配独立 Session。

## 2. Node 内部层级

```text
Node（node_id）
 ├─ Agent A（agent_id）
 │   ├─ personal session
 │   └─ workgroup session(s)
 ├─ Agent B
 └─ 临时子 Agent（父 Agent 的短生命周期 session）
```

每个 Session 拥有自己的：

- 消息历史和 ContextInjection；
- Turn/Step 协调器、队列、取消栅栏和 HITL；
- SSE stream 与序号；
- terminal/异步工具关联；
- 工作组 Session 的外部关联字段。

Agent 级配置（模型、工具组、skills、policy 上限）可由多个 Session 读取快照，但不能共享可变的执行状态。

## 3. 配置与运行时

```text
config.yaml
  Node 监听、Manage 连接、启动参数
        ↓
Agent template/defaults
  默认模型、工具组、skills、hooks、压缩阈值
        ↓
Agent instance snapshot
  agent_id、名称、有效配置快照
        ↓
Session runtime
  messages、Turn/Step、pending HITL、stream、side effects
```

system prompt、API `tools` schema、skill metadata 和动态 ContextInjection 的边界不同：

- 稳定规则和工作区约定进入 system prompt；Session 身份、主机快照和其他运行时状态通过 request-only ContextInjection 注入；
- 工具定义通过模型 API 的 `tools` 字段发送，不在 system prompt 中复制完整 schema；
- skill 元数据在 context boundary 写入 system prompt，实时目录通过 `list_available_skills` 按需发现；正文在加载生效的 Step 作为独立 context message；
- 当前终端、附件、工具状态等运行时信息只在需要的 Session 中注入，不污染其他 Session。

这样既保持模型上下文连续，也避免一次会话的状态变更误伤同 Agent 的其他会话缓存。

## 4. Workgroup AgentRef

工作组选择 Node 已注册的 Agent，而不是创建一个影子 Agent：

```json
{
  "agent_id": "agt_xxx",
  "node_id": "node_prod_01",
  "display_name": "代码审查员",
  "workgroup_id": "wg_xxx",
  "member_id": "member_xxx"
}
```

Manage 保存引用、ACL、Timeline 和 Assign；Node 保留 Agent 的 prompt、tools、skills、memory、LLM 和实际执行能力。Workgroup 侧只能以 policy overlay 收紧权限。

同一个 Agent 加入多个工作组时，每组使用独立 Session；成员消息不能写入 Agent 的个人对话 transcript。成员被归档只关闭该 Workgroup Session，不删除本地 Agent。

## 5. 并发不变量

1. 一个 Session 同时只有一个 active turn；所有 human、resume、tool result、async callback 进入同一个有序队列。
2. 不同 Session 可以并行，但共享工作区的非幂等副作用需要由 policy 或资源锁控制。
3. cancel 必须绑定 `session_id + turn_id + generation`，过期请求不能取消新一轮。
4. SSE、hydrate、HITL 和 terminal 读取必须按 Session 过滤。
5. Agent 归档先停止新 Session，再回收 runtime；历史按产品策略保留只读。

## 6. 代码映射

| 概念 | 代码 |
|---|---|
| Agent 存储/配置快照 | `node/internal/agentstore/`、`node/internal/api/agents.go` |
| Session 管理 | `node/internal/session/` |
| Turn/Step | `node/internal/turn/`、`node/internal/turn/coordinator.go` |
| Agent 目录注册 | `node/internal/manage/registrar.go` |
| Workgroup Session | `node/internal/workgroup/`、`manage/workgroup/` |
| 外部 API | [`docs/architecture/agent-node-api.md`](../architecture/agent-node-api.md) |

## 7. 迁移说明

- 旧 `session_id` 出现在 API 或数据库中时，按当前 1:1 Agent/Session 兼容映射处理；新文档优先使用 Agent/Session 的实际语义。
- 旧 `member.provision` / `WorkerBinding` 是迁移期兼容协议；新 UI 和新接口应使用 AgentRef + `agent.session.open`。
- 旧 A2A inbox、远程 Placement 和产品沙箱不再作为实现目标。
