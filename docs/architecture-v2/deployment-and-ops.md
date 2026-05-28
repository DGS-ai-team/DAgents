# 部署与运维指南

## 1. 部署拓扑

```
                            ┌──────────────┐
                            │   用户终端    │
                            └──────┬───────┘
                                   │
                            ┌──────┴───────┐
                            │    Nginx      │  ← 反向代理 + SSL 终结 + session hash 路由
                            │  :443/:80     │
                            └──┬───┬───┬───┘
                               │   │   │
                  ┌────────────┼───┘   └──────────────┐
                  ▼            ▼                      ▼
           ┌──────────┐ ┌──────────┐          ┌──────────────┐
           │Python    │ │Python    │          │Register      │
           │Backend 1 │ │Backend 2 │          │Center        │
           │:8000     │ │:8001     │          │:8010         │
           │          │ │          │          │              │
           │Agent A   │ │Agent C   │          │ 独立部署      │
           │Agent B   │ │Agent D   │          │ 可多副本      │
           └────┬─────┘ └────┬─────┘          └──────────────┘
                │            │
     ┌──────────┴───┐    ┌───┴──────────┐
     │Go Proxy C    │    │Go Proxy D    │
     │:9090         │    │:9090         │
     │Server 2012   │    │RHEL 6        │
     │kubectl/helm  │    │mysql/cron    │
     └──────────────┘    └──────────────┘
```

## 2. 组件清单

| 组件 | 进程 | 端口 | 交付方式 | 依赖 |
|------|------|------|----------|------|
| Nginx | nginx | 80/443 | 系统包 | 无 |
| Python Backend | `python run_agent_api.py` | 8000 | 源码或 PyInstaller | Python 3.11+ |
| Register Center | `python run_register_center.py` | 8010 | 源码（与 Backend 同仓库） | Python 3.11+ |
| Go Proxy | `./dagents-proxy` | 9090 | Go 单二进制 | 无（静态编译） |
| Go TUI | `./dagents-tui` | — | Go 单二进制 | 无 |

## 3. 配置文件分层

```
agent-config/
├── backend/
│   ├── base.yaml              # 所有后端实例共享
│   │   ├── llm_endpoint
│   │   ├── llm_api_key
│   │   └── shared_prompt     # 所有 Agent 共享的 system_prompt 基础
│   │
│   ├── instances/
│   │   ├── instance-1.yaml    # 实例 1 特有
│   │   │   ├── host: 0.0.0.0
│   │   │   ├── port: 8000
│   │   │   └── agents: [agent-a, agent-b]
│   │   │
│   │   └── instance-2.yaml
│   │       ├── host: 0.0.0.0
│   │       ├── port: 8001
│   │       └── agents: [agent-c, agent-d]
│   │
│   └── agents/
│       ├── agent-a.yaml       # Agent A 配置
│       │   ├── agent_id: "code-review-01"
│       │   ├── agent_type: "server"
│       │   ├── discovery_group: ["engineering"]
│       │   ├── capabilities: ["code-review", "git"]
│       │   └── custom_md_path: "/etc/dagents/agent-a/custom.md"
│       │
│       └── agent-d.yaml       # Agent D（终端类）
│           ├── agent_id: "k8s-ops-01"
│           ├── agent_type: "terminal"
│           ├── discovery_group: ["production"]
│           ├── capabilities: ["kubernetes", "helm"]
│           └── schedulable: true
│
├── proxy/
│   ├── proxy-c.yaml           # Go Proxy C 配置
│   │   ├── agent_id: "k8s-ops-01"
│   │   ├── backend_url: "http://10.0.0.1:8000"
│   │   ├── fs_root: "/opt/dagents/workspace"
│   │   ├── allowed_commands: ["kubectl", "helm", "docker"]
│   │   └── schedulable: true
│   │
│   └── proxy-d.yaml
│       ├── agent_id: "sql-query-01"
│       ├── backend_url: "http://10.0.0.1:8001"
│       └── schedulable: false     # 个人助手，不参与 A2A
│
└── tui/
    └── tui-alice.yaml
        ├── backend_url: "http://10.0.0.1:8000"
        ├── agent_id: "k8s-ops-01"
        └── theme: "dark"
```

## 4. 启动顺序

```
Phase 1 — 基础设施（依赖最少，先启动）：
  Register Center:
    python run_register_center.py

Phase 2 — 计算层（依赖 RC，可并行启动）：
  Python Backend 实例:
    python run_agent_api.py  # instance-1
    python run_agent_api.py  # instance-2
    ...
  各实例启动后自动向 RC 登记

Phase 3 — 执行层（依赖 Backend，按需启动）：
  Go Proxy（各宿主机独立）:
    ./dagents-proxy --config proxy-c.yaml
    ./dagents-proxy --config proxy-d.yaml

Phase 4 — 展示层（依赖 Backend，用户侧启动）：
  Go TUI:
    ./dagents-tui --config tui-alice.yaml

依赖关系：
  RC 挂了 → 已有 session 不受影响，新 Agent 发现暂停
  Backend 挂了 → 该实例上的 session 丢失（SQLite 可恢复）
  Proxy 挂了 → 该终端 Agent 不可调度，Backend 发告警
  TUI 挂了 → 用户重连即可
```

## 5. 健康检查

| 组件 | 检查端点 | 频率 | 告警条件 |
|------|----------|------|----------|
| Register Center | `GET /health` | 15s | 连续 3 次失败 |
| Python Backend | `GET /health` | 15s | 连续 3 次失败 |
| Go Proxy | `POST /v1/proxy/heartbeat` | 30s（主动推送） | 90s 无心跳 → 标记 offline |
| Go Proxy 工具可用 | `POST /execute` 健康检查命令 | 随心跳附带 | 工具执行失败率 > 10% |
| Go TUI | SSE 连接状态 | 实时 | 断开 → 通知用户 |
| LLM API | Backend 内部记录 | 每次调用 | 错误率 > 5% 或 P99 > 30s |
| A2A 消息投递 | RC relay 指标 | 每次 relay | 失败率 > 10% |

## 6. Nginx 配置示例

```nginx
upstream dagents_backend {
    # session 亲和性：同一 session 始终路由到同一后端实例
    hash $arg_session_id consistent;
    server 10.0.0.1:8000 max_fails=3 fail_timeout=30s;
    server 10.0.0.2:8001 max_fails=3 fail_timeout=30s;
}

upstream register_center {
    server 10.0.0.1:8010;
    server 10.0.0.2:8010 backup;  # 备机
}

server {
    listen 443 ssl;
    server_name agents.example.com;

    # SSE 专用：长连接，禁用缓冲
    location /v1/streams {
        proxy_pass http://dagents_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;           # 关闭缓冲，实时推送
        proxy_read_timeout 3600s;      # 最长 1 小时 SSE 连接
        chunked_transfer_encoding on;
    }

    # 常规 API
    location /v1/ {
        proxy_pass http://dagents_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 120s;
    }

    # Go Proxy 注册和心跳
    location /v1/proxy/ {
        proxy_pass http://dagents_backend;
        proxy_read_timeout 30s;
    }

    # Register Center
    location /rc/ {
        rewrite ^/rc/(.*) /$1 break;
        proxy_pass http://register_center;
    }
}
```

## 7. 扩容策略

| 阶段 | 并发 session | 并发 SSE | 后端实例 | 是否需要外部存储 |
|------|:-----------:|:--------:|:--------:|:----------------:|
| P1 起步 | < 100 | < 200 | 1 | ❌ SQLite 足够 |
| P2 成长 | 100-500 | 200-1000 | 2-3（Nginx hash） | ❌ SQLite + 健康检查即可 |
| P3 规模化 | 500-2000 | 1000-5000 | 5+ | ✅ Redis session store + RC etcd |
| P4 大规模 | 2000+ | 5000+ | 10+ | ✅ Redis + etcd + Kafka 解耦 |

**当前阶段的扩容只需要：**
1. 新增 Python Backend 实例（启动即用，向 RC 注册）
2. 在 Nginx upstream 列表中添加新实例
3. 无需外部存储、无状态迁移、无代码改动

## 8. 监控指标（Prometheus）

DAgents 已有 `/metrics` 端点，v2 扩展以下指标：

```python
# 新增指标
agent_connections_total{type="user_tui|a2a_caller|a2a_callee|proxy"}  # 连接数
agent_proxy_online_total                                                # 在线 Proxy 数
agent_proxy_last_heartbeat_seconds{agent_id}                           # 最后心跳时间
agent_tool_exec_latency_seconds{agent_type="server|terminal"}           # 工具执行延迟
agent_session_queue_depth{session_id}                                   # 各 session 队列深度
agent_a2a_peer_session_total{caller_agent, target_agent}               # A2A 会话数
agent_sse_push_latency_seconds                                          # SSE 推送延迟

# 告警规则
alert ProxyOffline:
  agent_proxy_last_heartbeat_seconds > 90

alert SessionQueueBackpressure:
  agent_session_queue_depth > 10

alert HighLLMErrorRate:
  rate(agent_llm_errors_total[5m]) / rate(agent_llm_requests_total[5m]) > 0.05

alert BackendDown:
  up{job="dagents-backend"} == 0
```

## 9. 日志与排障

```bash
# 各组件日志位置
Python Backend:  stdout/stderr  → 可选 systemd journal
Register Center: stdout/stderr  →  可选 systemd journal
Go Proxy:        stdout/stderr  →  可选写入文件 ./dagents-proxy.log
Go TUI:          本地日志  ~/.dagents/tui.log

# 常见问题排查
## Proxy 连不上 Backend
  curl http://backend-addr:8000/health
  → 检查网络/防火墙/backend_url 配置

## Agent 未在 RC 中显示
  curl http://rc-addr:8010/v1/agents?discovery_group=production
  → 检查 DISCOVERY_GROUPS 配置、AGENT_PUBLIC_BASE_URL 是否可路由

## TUI SSE 断连
  → 检查 Nginx proxy_read_timeout 设置
  → 检查后端日志中 SSE 连接异常
```

## 10. 安全清单

| 措施 | 说明 |
|------|------|
| TLS 终结 | Nginx 侧配置 SSL 证书，内网可选 |
| A2A token | `x-dagents-a2a-token` 头校验（已有） |
| Proxy 认证 | Proxy 注册时携带 agent 级别 token |
| FS_ROOT 沙箱 | Go Proxy 所有文件操作限于配置的根目录 |
| 命令白名单 | Go Proxy 可配置允许执行的命令列表 |
| 网络隔离 | Go Proxy 仅需访问 Backend，无需暴露公网端口 |
