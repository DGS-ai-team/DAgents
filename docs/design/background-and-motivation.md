# 重构背景与动机

> **已收敛至项目手册** → [../handbook/01-愿景与架构.md](../handbook/01-愿景与架构.md) §1 · [handbook/README.md](../handbook/README.md)

## 1. 问题：老旧 OS 上 Python Agent 栈不可行

DAgents 早期由 **Python Agent API** 承载本地助手。该栈要求目标机稳定运行 Python 3.11+、大量依赖与现代终端（textual），在下列环境中部署成本高或交互不可靠：

| 环境 | 主要限制 | 结论 |
|------|----------|------|
| Windows Server 2012 | 老 conhost 对 ANSI、鼠标上报、OSC 52 支持差 | Python 可安装但 textual TUI 不可靠 |
| Windows Server 2012 R2 | Python 3.12 是最后支持版本，终端能力仍弱 | 不宜作为 **Python Agent + TUI** 统一部署目标 |
| RHEL 6 / CentOS 6 | glibc 2.12，现代 Python wheel 不兼容 | 很难部署 Python 3.11+ |
| RHEL 7 / CentOS 7 | glibc 2.17，依赖包兼容性受限 | Python Agent 维护成本高 |
| 麒麟 V10 / Ubuntu 20.04+ / macOS | 现代运行时支持较好 | 适合运行 **Manage** 或现代运维机 |

这些“不适合跑 **完整 Python Agent**”的机器，往往又拥有不可替代的本地能力：

- 老 Windows 上的 AD 域控、PowerShell 运维脚本、Windows 专属工具。
- RHEL 6 上的历史数据库、cron 任务、本地配置和遗留业务系统。
- 只能在宿主机访问的 kubeconfig、内网 DNS、数据库 socket、本地密钥和 `custom.md`。

这些能力不应该因为 Python 运行时不可达而被排除在 Agent 网络之外。

## 2. 当前架构瓶颈

### 2.1 TUI 与 Python 进程绑定

当前 Python TUI 依赖 textual。textual 适合现代终端，但对老 Windows conhost、旧 SSH 客户端和受限终端支持不足。即使 Agent 后端可以运行，交互体验也可能不可用。

### 2.2 工具执行与 Backend 本机绑定

现有工具执行通常通过 Backend 本地进程、文件系统和 shell 完成。这意味着 Agent 只能使用 Backend 所在机器上的工具链，无法自然使用 **目标老旧服务器** 上的 PowerShell、专用 CLI 或本地文件。

### 2.3 分发形态受 Python 依赖约束

源码安装和 PyInstaller 都不能完全绕过目标 OS 的 libc、终端、系统库和安全策略限制。对于旧 glibc 或老 Windows，继续把 **完整 Python Agent** 搬过去会持续遇到兼容性问题（详见 [../os-compatibility.md](../os-compatibility.md)）。

## 3. 新洞察：Agent 本体下沉到 Go，Python 只做 Manage

v2 **重设计** 不再把「思考」留在 Python Backend、「行动」放在出站 Proxy，而是采用现网三组件：

```text
Agent Node（Go，部署在目标机或同网机器）
  ├─ LLM / turn loop / 工具执行 / 会话持久化
  ├─ 本地 shell、文件系统、skills、触发器
  └─ 出站：Manage 注册、心跳、审计

Client（Go TUI，与 Node 同包）
  └─ 仅连接本机 Node（127.0.0.1）

Manage（Python，部署在较新环境）
  └─ 注册、发现、审计、运维视图
```

**为什么 Go 放在老旧服务器上可行：**

- Go 可交叉编译为 **静态或近静态二进制**，对 glibc 要求可随构建镜像控制（见规划中的 `go-node-compatibility.md`）。
- **不依赖** 目标机安装 Python 3.11+、textual、大量 pip 依赖。
- **Client TUI 也用 Go**，避免 conhost/textual 问题；运维交互与 Agent 分离，Node 可无头运行。

**Python 保留价值：**

- **Manage** 跑在较新 Linux/容器/cloud；与 Go Node 通过 HTTP 协作。
- 不要求 Manage 部署在 RHEL 6 等极限环境。

## 4. 网络模型：Node 仅出站 Manage；A2A 不经 peer 直连

| 路径 | 方向 | 说明 |
|------|------|------|
| Node → Manage | 出站 HTTP | 注册、心跳、审计、**A2A send/inbox/reply** |
| Client → Node | 本地 HTTP/SSE | 永远 localhost |
| Agent A ↔ Agent B | **仅经 Manage** | discover + inbox；**禁止** A→B 直连 |
| 运维 → Manage | 查询 | Agent 列表、审计 |

所有 Node 对 Manage **仅需出站可达**；peer 之间 **无需** 互相开放端口，利于 NAT/防火墙与老旧内网环境。协议见 [a2a-via-manage.md](./a2a-via-manage.md)。

## 5. 设计目标

| 目标 | 说明 |
|------|------|
| **老旧 OS 可部署 Agent** | 目标机运行 **Agent Node + Client** Go 二进制，而非 Python Agent 栈 |
| **本地能力完整** | shell/fs/遗留 CLI 在 Node 进程内执行 |
| **运维可观测** | 所有 Node **必须** 注册 Manage；审计 **主动上报** |
| **Client 体验** | Go TUI 绑定本地 Node，配置同包 |
| **Python 范围收敛** | 代码库仅保留 **Manage**；Brain 迁移至 Go |
| **策略与审计** | 策略在 Node 执行；Manage 集中存储审计与可选策略版本 |

## 6. 非目标（当前阶段）

- 不在 Manage 上运行 LLM turn loop。
- 不要求 Client 连接 Manage 或远程 Node。
- 不保留 Python textual TUI 作为主线。
- 不保留「Python Backend + 出站 Proxy control channel」模型。
- Phase 1 不要求完整多租户、联邦 Manage（可单实例 Manage）。

## 7. 相关文档

- [three-component-model.md](./three-component-model.md) — 三组件边界  
- [manage-architecture.md](../design/manage-architecture.md) — Manage API  
- [../os-compatibility.md](../os-compatibility.md) — Python/Manage 构建兼容参考  
- [../handbook/README.md](../handbook/README.md) — 手册索引
