# Architecture v2 设计文档

本目录描述 DAgents v2 的目标架构：**Python 后端负责 Agent 大脑与控制平面，Go Proxy 负责跨平台执行平面，所有远程执行通过 Proxy 主动出站建立的控制通道完成**。

v2 的目标不是把现有 Python Agent 重写成 Go，而是把 Agent 能力拆成可独立部署、独立扩展、独立加固的几层：

- **Brain Layer**：LLM 推理、上下文、Agent turn loop、A2A 协作。
- **Control Plane**：Backend API、会话与连接协调、ProxyManager、工具路由、策略判定。
- **Execution Plane**：Go Proxy 在宿主机执行 shell、文件、系统工具和本地能力。
- **Client Plane**：Python TUI、Go TUI、Web UI 等用户入口。

## 核心架构决策

1. **Proxy 只出站连接 Backend**
   Go Proxy 不要求暴露公网端口，也不要求 Backend 能直连宿主机。Proxy 启动后主动注册到 Backend，并保持一条长连接控制通道；Backend 通过该通道下发执行任务。

2. **v2 目标支持共享状态多 Backend**
   Backend 可以多副本部署。连接、会话、Proxy presence、短期任务状态、SSE routing 等跨实例状态进入共享状态层，而不是依赖单机内存或 Nginx sticky session。

3. **Register Center 负责发现，不负责执行路由**
   RC 可以存储并返回 `agent_type`、`schedulable`、`host_info`、`capabilities` 等 Agent 元数据，但不决定工具在哪执行。执行路由由 Backend 的 Control Plane 根据 Agent 类型、会话归属、Proxy 状态和策略结果决定。

4. **所有外部可见 ID 由服务端生成**
   `session_id`、`connection_id`、`client_id`、`proxy_connection_id` 等 ID 不能由调用方自造。A2A 会话也必须由目标 Backend 创建并授权。

5. **远程执行采用策略审批模型**
   工具执行不是全部自动，也不是全部人工。策略层根据工具、参数、调用来源、Agent、session、风险等级返回 `auto`、`require_approval` 或 `deny`，并写入审计日志。

## 文档索引

| 文件 | 说明 |
|------|------|
| [background-and-motivation.md](./background-and-motivation.md) | 重构背景、OS 兼容性痛点、为什么需要出站 Proxy 控制通道 |
| [runtime-split.md](./runtime-split.md) | Brain / Control / Execution / Client 四层职责边界 |
| [agent-dual-runtime.md](./agent-dual-runtime.md) | Agent 双运行时目标架构、Proxy 生命周期、工具执行流程、RC/Backend 边界 |
| [identity-and-session.md](./identity-and-session.md) | agent、connection、session、client、proxy identity 与共享状态模型 |
| [security-and-policy.md](./security-and-policy.md) | 远程执行策略、审批、审计、沙箱和认证要求 |
| [deployment-and-ops.md](./deployment-and-ops.md) | 多 Backend 部署、共享状态、健康检查、监控、扩容与排障 |

## 分阶段落地

### Phase 1：单 Backend + 出站 Proxy MVP

- Backend 增加 ProxyManager 和 Proxy 注册接口。
- Go Proxy 主动连接 Backend，建立控制通道。
- 支持最小工具集：`shell_exec`、`file_read`、`file_write`。
- 所有 ID 改为服务端生成，禁止外部传入任意 `client_id`。
- 策略层先支持静态配置：`auto / require_approval / deny`。

### Phase 2：共享状态多 Backend

- 引入 Redis 或等价共享状态层。
- connection、session、proxy presence、pending execution、SSE routing 进入共享状态。
- Backend 多副本可接收用户请求；执行任务可以路由到持有 Proxy control channel 的实例。
- RC 继续负责 Agent 发现，Backend 负责执行路由。

### Phase 3：生产化增强

- Go TUI 完整替代老旧终端上的 textual TUI。
- 策略审批支持更细粒度条件和动态风险分级。
- 审计日志进入集中存储。
- 增加多租户隔离、token 轮换、mTLS、Proxy 远程升级和高可用告警。

## 与现有文档的关系

`docs/architecture-v2` 描述计划中的 v2 目标形态。当前已实现系统仍以 `doc/architecture-and-flows.md`、`doc/a2a-and-register-center.md`、`doc/built-in-tools.md`、`doc/os-compatibility.md` 等现有文档和实际代码为准。

实现时应以本目录作为目标架构参考，并在每个 Phase 完成后同步更新 CHANGELOG 和当前实现文档。
