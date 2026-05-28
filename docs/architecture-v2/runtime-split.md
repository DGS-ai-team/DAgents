# 运行时与职责划分

v2 将 DAgents 拆分为四层：Brain Layer、Control Plane、Execution Plane、Client Plane。划分目标是让每层只承担自己最擅长的职责，并通过明确接口协作。

## 1. 四层模型

```text
Client Plane      用户交互：Python TUI / Go TUI / Web UI / API client
      │
      ▼
Control Plane     Backend API、session/connection、Body Binding、ProxyManager、策略、SSE routing
      │
      ▼
Brain Layer       LLM 推理、Agent turn loop、上下文、A2A reasoning
      │
      ▼
Execution Plane   backend-local body / proxy-hosted body / shell / 文件系统 / 宿主机能力
```

Brain Layer 和 Control Plane 都运行在 Python Backend 中，但职责不同：Brain 决定“要做什么”，Control Plane 决定“能不能做、交给哪个 Body 做、如何追踪结果”。Execution Plane 不思考，只执行经过授权的任务。

## 2. Brain Layer：Agent 大脑

**职责**：

- LLM 调用与工具选择。
- Agent turn loop 和多步 ReAct 编排。
- 上下文管理、压缩、历史记录。
- A2A 协作中的消息理解、回复生成和 peer 调用决策。
- 按 Agent 的 Brain Profile 组装最终 system prompt。

**不负责**：

- 判断 Proxy 在线状态。
- 直接管理连接、心跳和共享状态。
- 直接绕过策略执行远程命令。
- 直接访问 Body 文件系统或宿主机资源。

**主要实现位置**：

```text
app/core/main_agent/
app/context/
app/harness/history/
app/harness/queue/
app/schemas/
```

## 3. Control Plane：控制平面

**职责**：

- 对外 API：sessions、messages、streams、connections、proxy registration。
- 生成并校验 `session_id`、`connection_id`、`client_id`、`proxy_connection_id`。
- 维护 session、connection、Body presence、Proxy presence 和执行任务状态。
- 根据 Agent 的 Body Binding、Proxy 状态和策略结果路由工具执行。
- 调用策略层判断工具执行结果：`auto`、`require_approval`、`deny`。
- 将执行结果返回 Brain Layer，并通过 SSE 推送用户可见事件。
- 在多 Backend 部署下协调共享状态。

**主要实现位置**：

```text
app/harness/api/
app/harness/service/
app/proxy/
app/policy/            # v2 新增建议
app/observability/
```

## 4. Execution Plane：执行平面

Execution Plane 由 Agent 绑定的 Body 提供。每个 Agent Instance 同一时间只绑定一个 active Body。

### 4.1 backend-local Body 执行

`body.kind: "backend_local"` 表示 Body 位于 Python Backend 本机。工具通过 Backend 本地执行器运行，适合代码审查、文档生成、Backend 本机自动化等场景。

### 4.2 proxy-hosted Body 执行

`body.kind: "proxy_hosted"` 表示 Body 位于 Go Proxy 宿主机。Go Proxy 主动出站连接 Backend，并接收 Backend 通过控制通道下发的执行任务。

Go Proxy 负责：

- 扫描本机环境：OS、工具链、工作目录、可用能力。
- 执行已授权的 shell、文件和本地工具任务。
- 应用本地沙箱约束：`fs_root`、超时、输出限制、环境变量限制。
- 上报心跳、任务状态和执行结果。

Go Proxy 不负责：

- 调用 LLM。
- 维护 Agent 会话上下文。
- 参与 A2A 协议。
- 自行决定是否执行未授权任务。

## 5. Client Plane：用户入口

Client Plane 可以有多种实现：

| 客户端 | 适用场景 | 说明 |
|--------|----------|------|
| Python TUI | 现代终端、开发环境 | 保留现有 textual 体验 |
| Go TUI | 老 Windows、受限终端、零依赖分发 | 通过 HTTP/SSE 访问 Backend |
| Web UI | 浏览器访问、集中入口 | 复用 Backend API |
| API client | 自动化集成 | 直接调用 session/message API |

Client Plane 不直接访问 Go Proxy。所有用户输入先进入 Backend，由 Control Plane 和 Brain Layer 决定是否需要 Body 执行。

## 6. Python 与 Go 的边界

| 能力 | Python Backend | Go Proxy / Go TUI |
|------|----------------|-------------------|
| LLM 推理 | 是 | 否 |
| Agent turn loop | 是 | 否 |
| A2A 协议 | 是 | 否 |
| session/connection 管理 | 是 | 否 |
| 策略判定 | 是 | 执行本地硬约束 |
| shell 执行 | backend-local Body 本地执行 | proxy-hosted Body 远程执行 |
| 文件读写 | backend-local Body 本地执行 | proxy-hosted Body 限于 `fs_root` 执行 |
| TUI 渲染 | Python TUI 可选 | Go TUI 可选 |
| 跨旧 OS 分发 | 弱 | 强 |

## 7. 通信方式

```text
Client Plane ──HTTP/SSE──> Backend Control Plane
Go Proxy     ──outbound control channel──> Backend Control Plane
Backend      ──HTTP──> Register Center
Backend      ──Redis protocol──> Shared State
```

控制通道可以用 WebSocket、HTTP/2 stream 或等价长连接实现。Phase 1 可选择最简单可靠的实现，但语义必须保持一致：**连接由 Proxy 发起，任务由 Backend 通过已建立连接下发**。

## 8. 路由规则

```text
Brain Layer 产生 ToolCall
  │
  ▼
Control Plane 查询 Agent Body Binding、session、policy、Body presence
  │
  ├── body.kind == backend_local → Backend 本地工具执行器
  └── body.kind == proxy_hosted  → ProxyManager 通过 control channel 下发任务
```

Brain Layer 不需要知道工具是在本地还是远程执行，只接收统一的 `ToolResult`。执行位置、审批、超时、重试和审计都由 Control Plane 处理。
