# Architecture v2 设计文档

本目录描述 DAgents v2 的目标架构：**Agent 是一个可寻址的 Brain + Body 实例，Brain 统一运行在 Python Backend，Body 提供执行环境、资源边界和宿主机能力**。

v2 不再按本地执行或远程执行把 Agent 分成两类。所有 Agent 都使用同一个定义：

```text
Agent Instance = Brain Runtime + Brain Profile + Body Binding
```

- **Brain Runtime**：Python Backend 中共享的推理运行时，负责 LLM、Agent loop、上下文、A2A reasoning。
- **Brain Profile**：某个 Agent 的模型、提示词、工具选择、上下文与压缩策略。
- **Body Binding**：某个 Agent 绑定的唯一终端/宿主执行环境，提供文件系统、shell、本地资源和硬约束。
- **Tool Manifest**：某个 Agent 可用工具清单，每个工具通过 `tool.kind` 声明在 Backend 执行还是在 Body 执行。
- **Agent Instance**：A2A、session、Register Center 发现时面对的可寻址对象，由 `agent_id` 标识。

## 核心架构决策

1. **一个 Agent 只绑定一个 Body**
   一个 Agent Instance 同一时间只有一个 active Body。若同一类能力绑定到不同宿主机、不同资源、不同 prompt/resource/context/environment 基础设施，应注册为不同 Agent。

2. **A2A 面向 Agent Instance**
   A2A 调用目标是 `agent_id` 对应的 Agent Instance。调用方不关心目标 Agent 的工具最终在 Backend 还是 Body 执行，也不能绕过目标 Agent 的 Brain、session、policy 或 Body Binding。

3. **执行路由按 tool.kind，而不是 body.kind**
   同一个 Agent Instance 可以同时拥有 Backend 执行工具和 Body/终端执行工具。`tool.kind` 决定工具由 Backend executor 处理，还是下发到该 Agent 绑定的 Body；Body Binding 不再把整个 Agent 固定成某一种执行类型。

4. **Proxy 只出站连接 Backend**
   Body 通过 Go Proxy 暴露宿主机能力时，Go Proxy 不要求暴露公网端口，也不要求 Backend 能直连宿主机；Proxy 主动注册到 Backend 并保持控制通道。

5. **v2 目标支持共享状态多 Backend**
   Backend 可以多副本部署。connection、session、Body presence、Proxy presence、execution、SSE routing 等跨实例状态进入共享状态层。

6. **Register Center 负责发现，不负责执行路由**
   RC 可以存储并返回 `schedulable`、`host_info`、`capabilities`、`tools` 等 Agent 元数据，但不决定工具在哪执行。执行路由由 Backend Control Plane 根据 `tool.kind`、Body Binding、会话归属、Body 状态和策略结果决定。

7. **终端/Body 执行采用策略审批模型**
   工具执行不是全部自动，也不是全部人工。策略层根据工具、参数、调用来源、Agent、Body、session、风险等级返回 `auto`、`require_approval` 或 `deny`，并写入审计日志。

## 文档索引

| 文件 | 说明 |
|------|------|
| [background-and-motivation.md](./background-and-motivation.md) | 重构背景、OS 兼容性痛点、为什么需要出站 Proxy 控制通道 |
| [runtime-split.md](./runtime-split.md) | Brain / Control / Execution / Client 四层职责边界 |
| [agent-dual-runtime.md](./agent-dual-runtime.md) | 统一 Agent Instance 与 Body Binding 目标架构 |
| [agent-lifecycle-and-registration.md](./agent-lifecycle-and-registration.md) | Agent/Body 创建、Proxy 注册、模板初始化、配置同步与版本控制 |
| [brain-body-responsibilities.md](./brain-body-responsibilities.md) | prompt、resources、context、environment、skills、配置等 Brain/Body 归属清单 |
| [message-queue-and-execution-control.md](./message-queue-and-execution-control.md) | Session Queue、Agent Instance 路由与 Body execution 调度边界 |
| [temporary-child-agents.md](./temporary-child-agents.md) | 临时子 Agent 的创建、权限收缩、Body Binding、父子通信和生命周期 |
| [identity-and-session.md](./identity-and-session.md) | agent、body、connection、session、proxy identity 与共享状态模型 |
| [security-and-policy.md](./security-and-policy.md) | 远程执行策略、审批、审计、沙箱和认证要求 |
| [deployment-and-ops.md](./deployment-and-ops.md) | 多 Backend 部署、共享状态、健康检查、监控、扩容与排障 |

## 分阶段落地

### Phase 1：单 Backend + 单 Body Binding MVP

- Backend 增加 AgentTemplate、Body Binding 元数据、Tool Manifest 和执行路由层。
- Go Proxy 主动连接 Backend，先注册 Body，再由 Backend 绑定已有 Agent 或按模板创建新 Agent。
- 支持最小工具集：Backend 工具与 Body 工具都通过 `tool.kind` 声明执行位置。
- 终端类工具先支持：`shell_exec`、`file_read`、`file_write`。
- 运行期 ID 由服务端生成，SSE 授权基于 `connection_id + session_id`，不再引入独立客户端订阅 ID。
- 策略层先支持静态配置：`auto / require_approval / deny`。
- 保留 per-session MessageQueue 作为 Session Queue，新增最小 Execution Dispatcher 区分 Brain turn、Backend tool execution 与 Body tool execution。
- 支持最小临时子 Agent：`backend_only` 或 `inherit_parent_body`、不可公开发现、权限为父 Agent 子集、TTL 自动清理。

### Phase 2：共享状态多 Backend

- 引入 Redis 或等价共享状态层。
- connection、session、body presence、proxy presence、pending execution、SSE routing 进入共享状态。
- Backend 多副本可接收用户请求；Body 工具执行任务可路由到持有 Proxy control channel 的实例。
- RC 继续负责 Agent 发现，Backend 负责 Tool Manifest、Body Binding 与执行路由。

### Phase 3：生产化增强

- Go TUI 完整替代老旧终端上的 textual TUI。
- Body profile 支持更完整的 resource、environment、skills manifest。
- 策略审批支持更细粒度条件和动态风险分级。
- 审计日志进入集中存储。
- 增加多租户隔离、token 轮换、mTLS、Proxy 远程升级和高可用告警。

## 与现有文档的关系

`docs/architecture-v2` 描述计划中的 v2 目标形态。当前已实现系统仍以 `docs/architecture-and-flows.md`、`docs/a2a-and-register-center.md`、`docs/built-in-tools.md`、`docs/os-compatibility.md` 等现有文档和实际代码为准。

实现时应以本目录作为目标架构参考，并在每个 Phase 完成后同步更新 CHANGELOG 和当前实现文档。
