# Agent 双运行时架构设计

本文提出 **DAgents 的多运行时架构**：Agent 的大脑（LLM 决策引擎）统一运行在 Python 后端，身体（工具执行环境）可以部署在本地或任意远程宿主机上由 Go Proxy 代理。该架构解决的核心问题见 [os-compatibility.md](./os-compatibility.md)——低版本 Windows（Server 2012）、旧 glibc Linux（RHEL 6）等无法直接运行 Python 3.11+ 的宿主机，其本地环境能力仍可作为 Agent 加入协作网络。

---

## 1. 核心概念：Agent = 大脑 + 身体

```
┌────────────────────────────────────────────────────┐
│                    Agent                           │
│                                                    │
│  ┌──────────────────┐    ┌──────────────────────┐ │
│  │  大脑 (Brain)     │    │  身体 (Body)          │ │
│  │                  │    │                      │ │
│  │  LLM 推理        │    │  工具执行环境         │ │
│  │  规划与决策       │    │  Shell / 文件系统     │ │
│  │  上下文管理       │    │  custom.md / skills  │ │
│  │  多步编排         │    │  数据库 / kubectl    │ │
│  │                  │    │  第三方工具链         │ │
│  │  运行位置:        │    │                      │ │
│  │  Python 后端      │    │  运行位置: 可变       │ │
│  └──────────────────┘    └──────────────────────┘ │
│                                                    │
│  大脑与身体的三种关系：                               │
│    A. 同在 Python 后端（非终端 Agent）                │
│    B. 大脑在后端，身体在远程 Go Proxy（终端 Agent）     │
│    C. 大脑在后端，身体同机但经本地 Go Proxy 隔离执行    │
└────────────────────────────────────────────────────┘
```

- **大脑**：永远在 Python 后端运行。负责 LLM 调用、Agent 循环、工具决策、A2A 协议。
- **身体**：提供工具实际执行所需的**文件系统、Shell 环境、工具链、权限域**。

---

## 2. Agent 分类

### 2.1 非终端 Agent（`agent_type: "server"`）

大脑与身体都在 Python 后端服务器上。与当前 DAgents 行为完全一致。

| 属性 | 值 |
|------|-----|
| 大脑位置 | Python 后端 |
| 身体位置 | Python 后端（本地磁盘、进程、工具） |
| schedulable | **始终 `true`**（天然可被其他 Agent 发现和调度） |
| 典型场景 | 代码审查、文档生成、通用对话 |

### 2.2 终端 Agent（`agent_type: "terminal"`）

大脑在 Python 后端，身体在远程宿主机的 Go Proxy 上。

| 属性 | 值 |
|------|-----|
| 大脑位置 | Python 后端（与其他 Agent 共享 Agent Engine） |
| 身体位置 | 远程宿主机上的 Go Proxy（Server 2012 / RHEL 6 / 麒麟 V10 / ...） |
| schedulable | **可配置**（注册时由宿主机 owner 决定是否开放给其他 Agent 发现） |
| 典型场景 | k8s 集群运维、数据库查询、CI/CD 触发、Windows 桌面自动化 |

终端 Agent 的 `schedulable` 字段允许宿主机的管理员决定：
- `schedulable: true` — Agent 加入 A2A 网络，能力被其他 Agent 通过 Register Center 发现
- `schedulable: false` — Agent 仅对绑定的终端可见，不参与 A2A 调度（纯个人助手模式）

---

## 3. 网络拓扑

```
                        ┌──────────────────────────┐
                        │     Register Center       │
                        │     (A2A 发现 + 路由)      │
                        └────────────┬─────────────┘
                                     │
        ┌────────────────────────────┼────────────────────────────┐
        │                            │                            │
        ▼                            ▼                            ▼
┌──────────────────────────────────────────────────────────────────┐
│                      Python Backend Server                        │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                    Agent Engine                              │ │
│  │                                                             │ │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌──────────┐ │ │
│  │  │ Agent A   │  │ Agent B   │  │ Agent C   │  │ Agent D  │ │ │
│  │  │ server    │  │ server    │  │ terminal  │  │ terminal │ │ │
│  │  │ sche:true │  │ sche:true │  │ sche:true │  │ sche:false│ │ │
│  │  │ 代码审查  │  │ 数据分析  │  │ k8s 运维  │  │ 个人助手 │ │ │
│  │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘  └────┬─────┘ │ │
│  │        │              │              │              │       │ │
│  └────────┼──────────────┼──────────────┼──────────────┼───────┘ │
│           │              │              │              │         │
│     工具本地执行    工具本地执行   ProxyManager   ProxyManager   │
│                                    │              │             │
└────────────────────────────────────┼──────────────┼─────────────┘
                                     │              │
                              HTTP/WebSocket     HTTP/WebSocket
                                     │              │
                          ┌──────────┴──┐    ┌──────┴──────────┐
                          │ Go Proxy C  │    │   Go Proxy D    │
                          │ Server 2012 │    │   RHEL 6        │
                          │             │    │                 │
                          │ kubectl     │    │ SQL DB          │
                          │ helm        │    │ cron jobs       │
                          │ kubeconfig  │    │ custom.md       │
                          │ custom.md   │    │ skills/         │
                          └─────────────┘    └─────────────────┘
```

**关键约束**：
- Register Center 不感知 Agent 的 `agent_type` 或身体位置。它只维护 `agent_id → message_endpoint` 映射。
- A2A 消息永远发到 Agent 的 `/v1/messages` 端点——该端点由 Python 后端对外暴露。
- Go Proxy 的内部地址（`execution_endpoint`）仅在 Python 后端内部使用，不暴露给 RC。

---

## 4. 一次跨终端 Agent 协作的完整流程

以终端 Agent C（k8s 运维, Server 2012）请终端 Agent D（数据分析, RHEL 6）查询数据库为例：

```
Step 1: Agent C 的大脑（Python 后端）收到用户消息
        → LLM 决策：需要查询数据库慢查询日志
        → capabilities 自检：本地无 SQL 工具
        → 调用 agent_discover: capabilities_hint=["sql","data-analysis"]

Step 2: Register Center 返回 Agent D 的记录
        → Agent D.agent_type="terminal", schedulable=true

Step 3: Python 后端 A2A 调用
        → agent_send_message → RC relay → Agent D 的 /v1/messages
        → 消息进入 Agent D 的消息队列

Step 4: Agent D 的大脑（Python 后端）推理
        → 需要执行 SQL: mysql -e "SELECT ..."
        → Agent D 的 body 在 RHEL 6 上
        → ProxyManager 路由: D → Go Proxy D

Step 5: Python 后端 ──HTTP──▶ Go Proxy D 的 /execute
        { "tool": "shell", "command": "mysql -e 'SELECT ...'" }

Step 6: Go Proxy D 在 RHEL 6 上执行
        → mysql 客户端查询本地数据库
        → 返回结果给 Python 后端

Step 7: Agent D 的大脑分析结果
        → LLM 生成回复
        → SSE 推送回 Agent C
```

**全程只有 Step 5-6 涉及远程宿主机调用。** Agent 的 LLM 推理、A2A 协议、上下文管理全部在 Python 后端完成。

---

## 5. Go Proxy 设计

Go Proxy 是部署在宿主机上的轻量执行代理。它**不思考、不决策、不调用 LLM**。

### 5.1 生命周期

```
启动 → 扫描环境 → 注册到 Backend → 心跳维持 → 接收/执行指令 → 退出时注销
```

### 5.2 功能清单（~400 行 Go）

| 功能 | 说明 |
|------|------|
| **环境扫描** | 启动时读取 `custom.md`、`skills/` 目录、检测可用工具链（kubectl / helm / mysql 等） |
| **向 Backend 注册** | `POST /v1/proxy/register`，上报 agent_id、capabilities、schedulable |
| **指令执行** | `POST /execute` 接收 shell 命令/文件操作，在本机执行并返回结果 |
| **心跳** | 定期向 Backend 报告存活，超时未心跳则 ProxyManager 标记其为离线 |
| **安全沙箱** | 可配置执行根目录（`FS_ROOT`）、命令白名单/黑名单、网络访问策略 |
| **状态上报** | 宿主机信息（OS、CPU、内存）、运行中的长时间任务 |

### 5.3 与 Backend 的通信协议

```
Go Proxy ──HTTP──▶ Python Backend

POST /v1/proxy/register     # 注册
  { "agent_id", "capabilities", "schedulable", "environment": {...} }

POST /v1/proxy/heartbeat     # 心跳（每 30s）
  { "agent_id" }

Python Backend ──HTTP──▶ Go Proxy

POST /execute                # 工具执行
  { "tool": "shell", "command": "...", "timeout": 30 }
  → { "exit_code": 0, "stdout": "...", "stderr": "" }

POST /execute                # 文件读取
  { "tool": "file_read", "path": "/etc/custom.md" }
  → { "content": "...", "encoding": "utf-8" }
```

---

## 6. ProxyManager（Python 后端新增模块）

`app/proxy/` 负责管理所有 Go Proxy 连接：

```python
class ProxyManager:
    """管理所有 Go Proxy 的连接池和路由。"""

    proxies: dict[str, ProxyConnection]  # agent_id → 连接

    async def register(self, info: ProxyRegisterRequest) -> AgentRecord
    async def heartbeat(self, agent_id: str) -> None
    async def execute(self, agent_id: str, tool: ToolCall) -> ToolResult
    async def check_offline(self) -> list[str]  # 心跳超时检测
```

当 Agent 的大脑决策需要执行工具时，ProxyManager 根据 `agent_id` 决定路由：

```python
def route_tool_execution(agent: AgentRecord, tool_call: ToolCall) -> ToolResult:
    if agent.agent_type == "server":
        # 非终端 Agent：直接在后端本地执行
        return local_tool_map[tool_call.name](tool_call.params)
    else:
        # 终端 Agent：转发到 Go Proxy
        return proxy_manager.execute(agent.agent_id, tool_call)
```

---

## 7. Register Center 的变化

RC 的 A2A 协议**完全不变**。仅 Agent 注册信息增加字段：

```json
// 非终端 Agent（与现有行为一致）
{
  "agent_id": "code-review-01",
  "base_url": "http://backend:8000",
  "discovery_group": ["engineering"],
  "capabilities_hint": ["code-review", "git"],
  "agent_type": "server",
  "schedulable": true
}

// 终端 Agent（新增字段）
{
  "agent_id": "k8s-ops-01",
  "base_url": "http://backend:8000",
  "discovery_group": ["production"],
  "capabilities_hint": ["kubernetes", "helm", "docker"],
  "agent_type": "terminal",
  "schedulable": true,
  "host_info": {
    "os": "Windows Server 2012",
    "tools": ["kubectl", "helm"]
  }
}
```

`agent_type` 和 `schedulable` 为可选字段（默认 `server` / `true`），向后兼容现有注册。

---

## 8. 交付形态对比

```
                        非终端 Agent              终端 Agent (Server 2012)
                        
Backend Server          Python DAgents 完整版      Python DAgents 完整版（不变）
                        工具本地执行 ✓              工具→ProxyManager 路由

宿主机                  无要求                      Go Proxy 单二进制（10-15MB）
                                                     拖过去就跑，零依赖
```

| 交付物 | 位置 | 语言 | 大小 | 依赖 |
|--------|------|------|------|------|
| DAgents Backend | 现代服务器（麒麟 V10 / RHEL 8+ / Ubuntu） | Python | ~200MB (含依赖) | Python 3.11+, pip |
| Go Proxy | 任意宿主机（Server 2012 / RHEL 6） | Go | ~10-15MB | 无（静态编译） |
| Register Center | 与 Backend 同机或独立 | Python | 已含在 Backend 中 | 同 Backend |

---

## 9. 迁移路径

当前架构中，所有 Agent 都是非终端 Agent（`server` 类型）。引入终端 Agent 的迁移步骤：

1. **Backend 新增 `app/proxy/`**：ProxyManager + 路由逻辑。不修改现有 Agent 代码。
2. **Go Proxy 开发**：独立仓库 `dagents-proxy`，约 400 行 Go。
3. **Register Center 字段扩展**：`agent_type`、`schedulable`、`host_info`（可选，向后兼容）。
4. **宿主机部署**：在需要暴露能力的旧 OS 上启动 Go Proxy，注册到 Backend。

现有 Agent 不受影响。非终端 Agent 的工具执行路径完全不变。

---

## 10. 与其它文档的关系

| 文档 | 内容 |
|------|------|
| [architecture-and-flows.md](./architecture-and-flows.md) | 整体架构与主/分支业务流程 |
| [a2a-and-register-center.md](./a2a-and-register-center.md) | A2A 协议、Register Center API 与配置 |
| [os-compatibility.md](./os-compatibility.md) | 操作系统兼容性清单与 glibc 说明 |
| [built-in-tools.md](./built-in-tools.md) | 内置工具清单与注册方式 |

---

**说明**：本文为架构设计文档，实现细节以实际代码和 CHANGELOG 为准。
