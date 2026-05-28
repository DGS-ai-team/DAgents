# 部署与运维指南

本文描述 architecture-v2 的目标部署形态。Phase 1 可以从单 Backend 起步，但文档中的最终目标是共享状态多 Backend。

## 1. 目标拓扑

```text
                         ┌─────────────────────┐
                         │      Client Plane    │
                         │ Go TUI / Web / API   │
                         └──────────┬──────────┘
                                    │ HTTP/SSE
                                    ▼
                         ┌─────────────────────┐
                         │  L7 Gateway / Nginx │
                         │ TLS / auth / routing│
                         └──────┬────────┬─────┘
                                │        │
                    ┌───────────▼──┐  ┌──▼───────────┐
                    │ Backend A    │  │ Backend B    │
                    │ Brain+Control│  │ Brain+Control│
                    └──────┬───────┘  └──────┬───────┘
                           │                 │
                           └──────┬──────────┘
                                  ▼
                         ┌─────────────────────┐
                         │    Shared State     │
                         │ Redis/session store │
                         └─────────────────────┘
                                  │
                                  ▼
                         ┌─────────────────────┐
                         │   Register Center   │
                         │ discovery / metadata│
                         └─────────────────────┘

 Agent A ── body.kind=backend_local ── Backend local executor
 Agent B ── body.kind=proxy_hosted  ── Go Proxy outbound channel
```

Go Proxy 只需要访问 Gateway 或 Backend 入口，不需要被 Backend 直连。

## 2. 组件清单

| 组件 | 职责 | 状态 |
|------|------|------|
| L7 Gateway / Nginx | TLS、认证入口、HTTP/SSE 转发 | 推荐生产部署 |
| Python Backend | Brain Layer + Control Plane | 可多副本 |
| Shared State | session、connection、body presence、proxy presence、execution 状态 | v2 多副本必需 |
| Register Center | Agent Instance 发现与元数据注册 | 可独立部署 |
| Go Proxy | `proxy_hosted` Body 的宿主机执行代理 | 每个 proxy-hosted Body 最多一个 active ProxyConnection |
| Go TUI | 跨平台用户终端 | 可选 |

## 3. 共享状态范围

多 Backend 目标形态下，以下状态不能只保存在单机内存：

| 状态 | 用途 |
|------|------|
| Backend presence | 判断实例是否在线、是否可接收新连接 |
| Connection records | 校验用户、A2A、proxy 连接归属 |
| Client records | SSE 授权与路由 |
| Session metadata | session 归属、TTL、connection 关联 |
| Body presence | 判断 Agent 绑定的 Body 是否可执行 |
| Proxy presence | 判断 proxy-hosted Body 的控制通道是否可用 |
| Execution records | 工具执行状态、审批、取消、超时 |
| Policy decisions | 审批决策与审计关联 |
| Event routing metadata | SSE 事件应该发给哪些 client |

上下文正文和历史记录可以按阶段从 SQLite 迁移到共享 session store。若仍使用 SQLite，必须明确 session owning Backend，并限制跨实例接管能力。

## 4. Backend 路由策略

Gateway 不应长期依赖 `$arg_session_id` sticky routing，因为很多 API 的 session 信息在 JSON body 中，SSE 和 A2A 也有不同路由需求。

推荐策略：

1. 任意 Backend 可以接收普通 API 请求。
2. Backend 根据共享状态判断请求资源归属。
3. 如果当前实例不是资源 owner：
   - 对短请求：内部转发给 owner 或通过共享状态完成操作。
   - 对 SSE：订阅共享事件总线或返回可重连信息。
   - 对 proxy-hosted Body execution：将任务投递给持有 control channel 的 Backend。
4. owner 不可用时，根据资源类型失败、重连或接管。

Phase 1 可以只部署单 Backend，避免过早实现跨实例转发。

## 5. Proxy 连接与路由

Proxy 连接流程：

```text
Go Proxy
  → POST /v1/proxy/register
  ← proxy_connection_id
  → 建立 outbound control channel
  → 心跳 / 状态上报 / 接收任务 / 返回结果
```

多 Backend 下：

- control channel 落在哪个 Backend，就由哪个 Backend 持有该连接。
- 共享状态记录 `body_id → proxy_connection_id → backend_instance_id`。
- 其他 Backend 需要执行该 Agent 的 proxy-hosted Body 工具时，通过共享状态或内部 RPC 将任务交给持有连接的 Backend。
- 持有连接的 Backend 下线时，Proxy 断线并重连到任意健康 Backend，获得新的 `proxy_connection_id`。

## 6. 启动顺序

```text
Phase 1 基础设施
  1. Shared State
  2. Register Center
  3. Python Backend 实例

Phase 2 执行层
  4. Go Proxy 启动并出站连接 Backend

Phase 3 客户端
  5. Go TUI / Python TUI / Web UI 连接 Backend
```

依赖影响：

| 故障 | 影响 |
|------|------|
| Shared State 故障 | 多 Backend 协调不可用，应进入降级或只读保护 |
| Register Center 故障 | 新 Agent 发现和注册受影响，已有 session 可继续 |
| Backend 实例故障 | 该实例持有的 SSE/Proxy control channel 断开，客户端和 Proxy 重连 |
| Go Proxy 故障 | 对应 proxy-hosted Body 不能执行远程工具 |
| Gateway 故障 | 外部访问不可用，内部 Backend 状态不一定受影响 |

## 7. 健康检查

| 组件 | 检查方式 | 告警条件 |
|------|----------|----------|
| Gateway | HTTP health | 连续失败 |
| Backend | `GET /health` + shared state heartbeat | 实例心跳过期 |
| Shared State | ping/read/write smoke test | 延迟过高或不可写 |
| Register Center | `GET /health` | 连续失败 |
| Go Proxy | control channel heartbeat | 超过 90s 无心跳 |
| Execution | execution timeout / failure rate | 超时率或失败率超过阈值 |
| LLM API | Backend 内部指标 | 错误率或 P99 超阈值 |
| SSE | client reconnect / push latency | 重连率或延迟异常 |

## 8. 监控指标

建议新增指标：

```text
dagents_backend_instances_online
dagents_connections_total{type}
dagents_sessions_total{body_kind}
dagents_body_online_total{body_kind}
dagents_proxy_online_total
dagents_proxy_heartbeat_age_seconds{agent_id,body_id}
dagents_proxy_control_channels{backend_instance_id}
dagents_execution_total{tool,target,status,body_kind}
dagents_execution_latency_seconds{tool,target,body_kind}
dagents_execution_queue_depth{agent_id,body_id}
dagents_policy_decisions_total{decision,tool,body_kind}
dagents_approval_pending_total
dagents_sse_clients_total
dagents_sse_push_latency_seconds
dagents_a2a_sessions_total{caller_agent,target_agent,status}
```

关键告警：

- Proxy heartbeat age > 90s。
- execution timeout rate 持续升高。
- pending approval 长时间无人处理。
- Shared State 写入失败或延迟过高。
- Backend 实例数低于期望副本数。
- SSE 重连率异常。

## 9. 配置分层

```text
agent-config/
├── backend/
│   ├── base.yaml              # LLM、shared state、RC、策略默认值
│   ├── instances/             # backend_instance_id、host、port
│   └── agents/                # agent_id、brain_profile、body.kind、body_id、capabilities、schedulable
├── proxy/
│   └── proxy.yaml             # agent_id、body_id、backend_url、fs_root、本地硬约束
├── policy/
│   └── execution-policy.yaml  # 工具审批和拒绝规则
└── tui/
    └── tui.yaml               # backend_url、agent_id、主题
```

敏感配置如 LLM API key、Proxy token、A2A token 应通过 secret 管理或环境变量注入，不应写入普通配置文件。

## 10. 扩容阶段

| 阶段 | 形态 | 说明 |
|------|------|------|
| Phase 1 | 单 Backend + 单 Shared State 可选 | 验证 outbound Proxy 和策略执行 |
| Phase 2 | 多 Backend + Redis/shared state | 支持跨实例 session、Body presence、Proxy presence 和 execution tracking |
| Phase 3 | 多 Backend + 集中审计 + 高可用 RC | 面向生产运维和多团队使用 |
| Phase 4 | 多租户 + 分区调度 + 消息总线 | 面向大规模 Agent 网络 |

## 11. 排障指引

### Proxy 连接不上 Backend

检查：

- Proxy 所在机器能否访问 `backend_url`。
- token 是否有效。
- Gateway 是否允许 WebSocket 或长连接升级。
- Backend 是否记录注册失败原因。

### proxy-hosted Body 在线但执行失败

检查：

- `body_id` 是否绑定到目标 Agent。
- `proxy_connection_id` 是否仍是 active。
- policy 是否返回 `deny` 或等待审批。
- Proxy 本地 `fs_root`、工作目录和命令白名单。
- execution timeout 和输出大小限制。

### SSE 没有事件

检查：

- `client_id` 是否有效且未过期。
- client 是否有 session 订阅权限。
- 当前 Backend 是否能订阅共享事件或转发到 SSE owner。
- Gateway 是否关闭 buffering，并允许长连接。

## 12. 安全运维要求

- Gateway 负责 TLS 终结和外部认证。
- Backend 校验所有 connection、session、client、body、proxy 身份。
- Proxy 使用 Agent/Body 级 token 注册，并支持轮换。
- 远程执行必须经过策略层并写审计。
- Body 本地执行必须限制 `fs_root`、超时、输出大小和环境变量。
- 生产环境应启用集中日志和审计保留策略。
