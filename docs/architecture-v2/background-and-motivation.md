# 重构背景与动机

## 1. 问题：Agent 能力受限于 Python 运行时可达范围

DAgents 当前主要由 Python 实现。Agent 的 LLM 推理、工具执行、文件访问、TUI 交互和 A2A 协作都运行在同一个 Python 运行时附近。这让系统简单，但也带来一个根本限制：**Agent 只能完整部署在能稳定运行 Python 3.11+ 及其依赖的机器上**。

现实环境中，大量机器无法满足这个条件：

| 环境 | 主要限制 | 结论 |
|------|----------|------|
| Windows Server 2012 | 老 conhost 对 ANSI、鼠标上报、OSC 52 支持差 | Python 可安装但 textual TUI 不可靠 |
| Windows Server 2012 R2 | Python 3.12 是最后支持版本，终端能力仍弱 | Agent 后端可勉强运行，TUI 不适合 |
| RHEL 6 / CentOS 6 | glibc 2.12，现代 Python wheel 不兼容 | 很难部署 Python 3.11+ |
| RHEL 7 / CentOS 7 | glibc 2.17，依赖包兼容性受限 | 可用但维护成本高 |
| 麒麟 V10 / Ubuntu 20.04+ / macOS | 现代运行时支持较好 | 适合运行 Python Backend |

这些“不适合跑 Python Agent”的机器，往往又拥有不可替代的本地能力：

- 老 Windows 上的 AD 域控、PowerShell 运维脚本、Windows 专属工具。
- RHEL 6 上的历史数据库、cron 任务、本地配置和遗留业务系统。
- 只能在宿主机访问的 kubeconfig、内网 DNS、数据库 socket、本地密钥和 `custom.md`。

这些能力不应该因为 Python 运行时不可达而被排除在 Agent 网络之外。

## 2. 当前架构瓶颈

### 2.1 TUI 与 Python 进程绑定

当前 Python TUI 依赖 textual。textual 适合现代终端，但对老 Windows conhost、旧 SSH 客户端和受限终端支持不足。即使 Agent 后端可以运行，交互体验也可能不可用。

### 2.2 工具执行与 Backend 本机绑定

现有工具执行通常通过 Backend 本地进程、文件系统和 shell 完成。这意味着 Agent 只能使用 Backend 所在机器上的工具链，无法自然使用另一台机器上的 `kubectl`、`mysql`、PowerShell、专用 CLI 或本地文件。

### 2.3 分发形态受 Python 依赖约束

源码安装和 PyInstaller 都不能完全绕过目标 OS 的 libc、终端、系统库和安全策略限制。对于旧 glibc 或老 Windows，继续把完整 Agent 运行时搬过去会持续遇到兼容性问题。

## 3. 关键洞察：思考与行动可以分离

一次 Agent turn 可以拆成两类行为：

```text
思考：接收消息、LLM 推理、规划、上下文管理、A2A 协作、SSE 推送
行动：执行 shell、读写文件、调用宿主机工具、访问本地环境能力
```

“思考”依赖 LLM SDK、上下文模型、消息队列和 Python 生态，适合集中在现代 Backend 上运行。
“行动”依赖宿主机环境、系统命令、本地权限和跨平台分发，适合由轻量 Go Proxy 在目标机器执行。

因此 v2 的核心方向是：**Agent 的大脑集中运行，Agent 的身体可分布部署**。

## 4. 为什么 Proxy 必须只出站连接 Backend

一种直观设计是 Backend 直接调用宿主机上的 Proxy：

```text
Backend ──HTTP──> Go Proxy /execute
```

这个模型实现简单，但在真实企业网络里问题很大：

- 老机器通常在 NAT、防火墙或专用内网后面，Backend 无法直接访问。
- 让每台宿主机暴露端口会增加运维和安全成本。
- 动态 IP、VPN、堡垒机和多网段会让服务发现变复杂。
- Proxy 作为执行端暴露入站接口，会扩大攻击面。

v2 因此采用出站控制通道：

```text
Go Proxy ──outbound control channel──> Backend
Backend 通过已建立通道下发任务
```

这个模型的优点是：

- Proxy 只需要能访问 Backend，不需要公网入站端口。
- 更容易穿过 NAT、防火墙和老旧网络环境。
- Backend 可以统一认证、限流、审计和策略判定。
- Proxy 失联可以通过心跳和连接状态直接发现。

## 5. 设计目标

| 目标 | 说明 |
|------|------|
| 跨 OS 执行能力 | 任何能运行 Go 静态二进制的宿主机都可以成为执行环境 |
| Python 核心最小扰动 | Agent loop、LLM runtime、A2A 语义尽量保持稳定 |
| 出站连接优先 | Proxy 不暴露公网端口，执行任务通过控制通道下发 |
| 多 Backend 可扩展 | v2 目标支持共享状态多副本，而不是长期依赖单机内存 |
| 策略化安全执行 | 远程工具执行经过策略判定、审批和审计 |
| 向后兼容 | `server` Agent 保持现有本地执行行为 |

## 6. 非目标

- 不把 LLM 推理迁移到 Go。
- 不让 Go Proxy 参与 Agent 决策或上下文管理。
- 不要求 Phase 1 一次实现完整多租户、高可用和集中审计。
- 不让 Register Center 参与具体工具执行路由。
