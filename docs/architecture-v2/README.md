# Architecture v2 设计文档

本目录描述 DAgents v2 的目标架构：**Agent 是一个可寻址的 Brain + Body 实例，Brain 统一运行在 Python Backend，Body 提供执行环境、资源边界和宿主机能力**。

v2 不再按本地执行或远程执行把 Agent 分成两类。所有 Agent 都使用同一个定义：

```text
Agent Instance = Brain Runtime + Brain Profile + Body Binding
```

- **Brain Runtime**：Python Backend 中共享的推理运行时，负责 LLM、Agent loop、上下文、A2A reasoning。
- **Brain Profile**：某个 Agent 的模型、提示词、工具选择、上下文与压缩策略。
- **Body Binding**：某个 Agent 绑定的唯一执行环境。Body 可以是 `backend_local` 或 `proxy_hosted`。
- **Agent Instance**：A2A、session、Register Center 发现时面对的可寻址对象，由 `agent_id` 标识。

## 核心架构决策

1. **一个 Agent 只绑定一个 Body**
   一个 Agent Instance 同一时间只有一个 active Body。若同一类能力绑定到不同宿主机、不同资源、不同 prompt/resource/context/environment 基础设施，应注册为不同 Agent。

2. **A2A 面向 Agent Instance**
   A2A 调用目标是 `agent_id` 对应的 Agent Instance。调用方不关心目标 Agent 的 Body 是本地还是远程，也不能绕过目标 Agent 的 Brain、session、policy 或 Body Binding。

3. **Proxy 只出站连接 Backend**
   `proxy_hosted` Body 通过 Go Proxy 暴露宿主机能力。Go Proxy 不要求暴露公网端口，也不要求 Backend 能直连宿主机；Proxy 主动注册到 Backend 并保持控制通道。

4. **v2 目标支持共享状态多 Backend**
   Backend 可以多副本部署。connection、session、Body presence、Proxy presence、execution、SSE routing 等跨实例状态进入共享状态层。

5. **Register Center 负责发现，不负责执行路由**
   RC 可以存储并返回 `body.kind`、`schedulable`、`host_info`、`capabilities` 等 Agent 元数据，但不决定工具在哪执行。执行路由由 Backend Control Plane 根据 Body Binding、会话归属、Body 状态和策略结果决定。

6. **远程执行采用策略审批模型**
   工具执行不是全部自动，也不是全部人工。策略层根据工具、参数、调用来源、Agent、Body、session、风险等级返回 `auto`、`require_approval` 或 `deny`，并写入审计日志。

## 文档索引

| 文件 | 说明 |
|------|------|
| [background-and-motivation.md](./background-and-motivation.md) | 重构背景、OS 兼容性痛点、为什么需要出站 Proxy 控制通道 |
| [runtime-split.md](./runtime-split.md) | Brain / Control / Execution / Client 四层职责边界 |
| [agent-dual-runtime.md](./agent-dual-runtime.md) | 统一 Agent Instance 与 Body Binding 目标架构 |
| [brain-body-responsibilities.md](./brain-body-responsibilities.md) | prompt、resources、context、environment、skills、配置等 Brain/Body 归属清单 |
| [identity-and-session.md](./identity-and-session.md) | agent、body、connection、session、client、proxy identity 与共享状态模型 |
| [security-and-policy.md](./security-and-policy.md) | 远程执行策略、审批、审计、沙箱和认证要求 |
| [deployment-and-ops.md](./deployment-and-ops.md) | 多 Backend 部署、共享状态、健康检查、监控、扩容与排障 |

## 分阶段落地

### Phase 1：单 Backend + 单 Body Binding MVP

- 将现有 Backend 本机执行建模为 `backend_local` Body。
- Backend 增加 Body Binding 元数据和执行路由层。
- Go Proxy 主动连接 Backend，注册 `proxy_hosted` Body 并建立控制通道。
- 支持最小工具集：`shell_exec`、`file_read`、`file_write`。
- 所有 ID 改为服务端生成，禁止外部传入任意 `client_id`。
- 策略层先支持静态配置：`auto / require_approval / deny`。

### Phase 2：共享状态多 Backend

- 引入 Redis 或等价共享状态层。
- connection、session、body presence、proxy presence、pending execution、SSE routing 进入共享状态。
- Backend 多副本可接收用户请求；执行任务可路由到持有 Proxy control channel 的实例。
- RC 继续负责 Agent 发现，Backend 负责 Body Binding 与执行路由。

### Phase 3：生产化增强

- Go TUI 完整替代老旧终端上的 textual TUI。
- Body profile 支持更完整的 resource、environment、skills manifest。
- 策略审批支持更细粒度条件和动态风险分级。
- 审计日志进入集中存储。
- 增加多租户隔离、token 轮换、mTLS、Proxy 远程升级和高可用告警。

## 与现有文档的关系

`docs/architecture-v2` 描述计划中的 v2 目标形态。当前已实现系统仍以 `docs/architecture-and-flows.md`、`docs/a2a-and-register-center.md`、`docs/built-in-tools.md`、`docs/os-compatibility.md` 等现有文档和实际代码为准。

实现时应以本目录作为目标架构参考，并在每个 Phase 完成后同步更新 CHANGELOG 和当前实现文档。
