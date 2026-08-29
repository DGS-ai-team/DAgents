# Linux 通道设计方案

> 状态：主路径已实现；本文保留 `linux_exec` 兼容语义。新 Agent 优先使用 `terminal_*`，旧工具名仅用于迁移旧 Agent 快照。
>
> 目标：在 Agent Node 侧配置多个 Linux 连接，将一个或多个连接绑定到不同 Agent，并允许 Agent 在审批和策略约束下通过 SSH 打开通道、执行命令和读取结果。

## 1. 目标与边界

### 1.1 目标

- 在 Node Web UI 中管理多个 Linux 连接配置；
- 支持不同 IP/域名、端口、Linux 用户和认证方式；
- 一个 Agent 可以绑定多个 Linux 通道；
- 一个 Linux 通道可以被多个 Agent 复用，但每个 Agent 使用独立的运行会话；
- Agent 只能看到和使用自己被绑定的通道；
- 通过 SSH 执行远程命令，返回 stdout、stderr、退出码和耗时；
- 支持长期交互通道的演进，但第一阶段先保证一次性命令执行可靠；
- 复用 DAgents 现有 Agent policy、HITL、SSE、审计和取消机制；
- 为未来的容器、Windows、Workgroup 远程执行保留统一 provider 接口。

### 1.2 非目标

第一阶段不做：

- 自己实现 SSH 协议；
- 将密码同步到 Manage；
- 让 Agent 直接获得私钥或密码；
- 让远程主机主动回连 Node；
- 把 Linux 通道自动暴露给所有 Agent；
- 用 SSH 通道替代 DAgents Workgroup；
- 在远程主机上安装常驻 DAgents 服务。

## 2. 与 Codex SSH 方案的关系

参考 Codex 的原则：

```text
本地 Agent/UI
      │
      │ SSH client / port forwarding / exec
      ▼
远程 Linux 用户进程
      │
      ├─ shell
      ├─ PTY（后续）
      └─ 文件/命令操作
```

但 DAgents 第一阶段不必立即复制 Codex 的远程 `exec-server`。推荐先采用：

```text
Agent Tool
   ↓
LinuxChannelProvider
   ↓
Go SSH Client
   ↓
远程 sshd
```

后续如果需要持久 PTY、远程文件 RPC、断线恢复，再把 Provider 的内部实现升级为独立 Exec Server；上层 Agent 工具协议保持不变。

## 3. 核心概念

需要明确区分三个对象：

### 3.1 LinuxChannelProfile：配置通道

表示一个可复用的远程连接配置，不代表当前已经建立 TCP/SSH 连接。

示例：

```text
prod-app-01
  host: 10.10.0.21
  user: deploy
  port: 22
  auth: credential-prod-deploy
```

### 3.2 LinuxCredential：认证凭据

独立于通道保存，用于避免密码和密钥跟着 Agent 配置传播。

建议支持：

- `password`：密码；
- `private_key`：私钥文件或密钥内容引用；
- 后续 `certificate`：SSH certificate。

用户要求的 IP、用户名、密码可以映射为 `host + username + credential(password)`，但设计上不把 password 直接放到 channel profile 或 Agent snapshot。

### 3.3 LinuxChannelSession：运行会话

表示某个 Agent 实际打开的远程 SSH 会话：

```text
agent_id + channel_id + session_id
```

它保存：

- SSH client/transport 状态；
- 当前远程工作目录；
- shell 类型；
- PTY 状态（后续）；
- 最近活动时间；
- 超时和最大并发信息。

不同 Agent 即使绑定同一个 channel，也必须使用不同 session，不能共享远程 cwd、stdin、PTY 或命令状态。

## 4. 推荐架构

```text
Web UI / Client
        │ REST/SSE
        ▼
Node API
  ├─ Channel Config API
  ├─ Credential API（不返回秘密）
  ├─ Agent Binding API
  └─ Session/API status
        │
        ▼
LinuxChannelManager
  ├─ ProfileStore
  ├─ CredentialStore
  ├─ BindingResolver
  ├─ ConnectionPool
  ├─ CommandExecutor
  ├─ Policy/HITL adapter
  └─ Audit/Event publisher
        │
        ▼
LinuxChannelProvider
  └─ SSH client implementation
        │
        ▼
远程 Linux sshd
```

Node 仍然是 Agent 执行主体。Manage 只在后续阶段接收通道的非敏感元数据或策略，不接收密码、私钥和可直接使用的 secret。

## 5. 数据模型

### 5.1 `linux_channels`

建议使用 Node 本地 SQLite：

```sql
CREATE TABLE linux_channels (
  channel_id       TEXT PRIMARY KEY,
  display_name     TEXT NOT NULL,
  host             TEXT NOT NULL,
  port             INTEGER NOT NULL DEFAULT 22,
  username         TEXT NOT NULL,
  credential_id    TEXT NOT NULL,
  host_key_policy  TEXT NOT NULL DEFAULT 'known_hosts',
  host_key_ref     TEXT,
  remote_shell     TEXT NOT NULL DEFAULT 'bash',
  default_cwd      TEXT,
  connect_timeout_ms INTEGER NOT NULL DEFAULT 10000,
  command_timeout_ms INTEGER NOT NULL DEFAULT 120000,
  keepalive_seconds INTEGER NOT NULL DEFAULT 30,
  max_sessions     INTEGER NOT NULL DEFAULT 4,
  enabled          INTEGER NOT NULL DEFAULT 1,
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL
);
```

`host_key_policy` 和 `host_key_ref` 是活动配置字段：`known_hosts` 使用 Node 本机 known_hosts（`host_key_ref` 可指定路径），`pinned` 使用 SHA256 主机指纹。敏感凭据仍不得放入通道记录。

不建议把 `password` 或私钥原文放在这张表中。

### 5.2 `linux_credentials`

```sql
CREATE TABLE linux_credentials (
  credential_id    TEXT PRIMARY KEY,
  display_name     TEXT NOT NULL,
  auth_type        TEXT NOT NULL,
  secret_ref       TEXT NOT NULL,
  username_hint    TEXT,
  enabled          INTEGER NOT NULL DEFAULT 1,
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL
);
```

`secret_ref` 保存引用或加密后的本地凭据。当前实现支持两种来源：

- `env:NAME`：从 Node 进程环境变量解析；
- `literal:<ciphertext>`：由直接输入接口生成，使用 Node runtime 的 SecretBox 加密，接口不接受调用方直接提交该内部格式。

直接输入的密码或私钥会保存到 Node 本地配置数据库，密文只在建立 SSH 连接时解密。环境变量引用仍然兼容旧配置，但修改环境变量后需要重启 Node 才能让进程看到新值。推荐优先级：

1. 操作系统 Secret Store/Keyring；
2. Node 本地加密 secret store，密钥由安装实例保护；
3. 环境变量引用；
4. 直接输入并加密保存本地凭据。

密码支持是产品需求，但不应成为默认推荐认证方式。生产环境默认推荐 SSH private key 或证书。

### 5.3 `agent_linux_channels`

```sql
CREATE TABLE agent_linux_channels (
  agent_id         TEXT NOT NULL,
  channel_id       TEXT NOT NULL,
  enabled          INTEGER NOT NULL DEFAULT 1,
  is_default       INTEGER NOT NULL DEFAULT 0,
  remote_cwd       TEXT,
  shell            TEXT,
  max_concurrency  INTEGER NOT NULL DEFAULT 1,
  approval_mode    TEXT NOT NULL DEFAULT 'require_approval',
  allowed_commands_json TEXT,
  denied_commands_json  TEXT,
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL,
  PRIMARY KEY (agent_id, channel_id)
);
```

Agent snapshot 中只保存 channel ID、绑定开关和非敏感执行选项；不保存 host password、private key、secret_ref 的可解析秘密。

也可以先按现有 MCP 模式把绑定写入 `config_snapshot.defaults.linux_channels.bindings`，但长期建议将绑定独立存储，以便实时禁用、审计和权限变更。

## 6. 认证与主机校验

### 6.1 认证方式

```text
password     → SSH password authentication
private_key  → 本地 key path / OS secret ref
certificate  → 后续扩展
```

SSH 连接建立时，只有 `LinuxChannelProvider` 能读取凭据。以下位置禁止出现秘密：

- Agent prompt；
- Agent config snapshot；
- SSE payload；
- tool result；
- 普通日志；
- Manage API；
- Git 仓库和导出的配置。

### 6.2 Host key 验证

Node 严格执行主机密钥校验，不提供 insecure fallback：

- `known_hosts`：读取 Node 当前系统用户的 `~/.ssh/known_hosts`，或使用 `host_key_ref` 指定路径；
- `pinned`：`host_key_ref` 保存 `SHA256:...` 主机指纹；
- 首次测试遇到未知主机时，后端只返回观测到的公钥指纹，不自动信任；用户通过可信渠道核对后，才能在 Web UI 中保存为 `pinned`；
- 未知密钥和已配置指纹不匹配分别返回 `host_key_unknown`、`host_key_mismatch`。

## 7. 通道生命周期

### 7.1 配置状态

```text
disabled
   ↓ enable
configured
   ↓ test/open
connecting
   ├─ error
   └─ open
```

### 7.2 运行会话状态

```text
closed
  ↓ open
connecting
  ├─ failed
  └─ open
       ├─ idle
       ├─ busy
       ├─ degraded/reconnecting
       └─ closing → closed
```

配置通道和运行会话必须分离：删除或禁用配置时，已有 session 应进入 draining/closing，不能继续接受新的命令。

### 7.3 第一阶段推荐行为

第一阶段采用“连接复用 + 命令级 SSH session”：

- 一个 channel profile 可以维护一个可复用 SSH client；
- 每次 `linux_exec` 创建独立的远程 SSH session；
- 命令结束后关闭该 SSH session；
- SSH client 可按空闲超时回收；
- 不在第一阶段开放跨命令共享 shell 状态。

这样能避免持久 shell 的 cwd、环境变量、后台进程和 stdin 状态泄漏。

### 7.4 第二阶段持久 PTY

需要交互式命令时再增加：

- `linux_open` 返回 `session_id`；
- `linux_write(session_id, data)`；
- `linux_read(session_id, after_seq)`；
- `linux_resize(session_id, cols, rows)`；
- `linux_close(session_id)`。

PTY session 必须绑定 `agent_id`、`session_id` 和 channel，不能仅凭 channel ID 操作。

## 8. Agent 工具设计

### 8.1 MVP 工具

建议先只暴露三个工具：

#### `linux_list_channels`

只返回当前 Agent 已绑定且有效的通道：

```json
{
  "channels": [
    {
      "channel_id": "prod-app-01",
      "display_name": "生产应用 01",
      "host": "10.10.0.21",
      "username": "deploy",
      "default_cwd": "/srv/app",
      "status": "configured"
    }
  ]
}
```

不返回密码、私钥、secret_ref 或完整认证信息。

#### `linux_exec`

```json
{
  "channel_id": "prod-app-01",
  "command": "git status --short",
  "cwd": "/srv/app",
  "timeout_ms": 30000,
  "max_output_bytes": 65536
}
```

返回：

```json
{
  "channel_id": "prod-app-01",
  "remote_user": "deploy",
  "exit_code": 0,
  "stdout": "...",
  "stderr": "",
  "duration_ms": 183,
  "truncated": false
}
```

`host`、`username` 和命令执行结果要按敏感等级处理，不能把密码或连接对象序列化到结果中。

#### `linux_channel_status`

用于查看连接状态、最近错误和 session 数量，不执行远程命令。

### 8.2 后续 PTY 工具

```text
linux_open
linux_write
linux_read
linux_resize
linux_close
```

模型默认不应直接管理复杂的 `session_id` 生命周期。更好的产品语义是：普通命令使用 `linux_exec`，只有明确需要交互式终端时才由模型或用户显式打开 PTY。

## 9. Policy 与 HITL

Linux 远程命令必须走现有 Tool Hook/Policy/HITL 链路，不能绕过 `PolicyToolHook`。

建议决策顺序：

```text
Agent 是否绑定 channel
        ↓
channel 是否 enabled
        ↓
linux_exec 是否在 Agent tool policy 中允许
        ↓
channel binding 的 command allow/deny
        ↓
命令风险分类
        ↓
HITL / auto / deny
        ↓
建立连接并执行
```

建议的默认策略：

| 操作 | 默认策略 |
|---|---|
| `linux_list_channels` | auto |
| 连接测试 | require approval |
| 只读命令，如 `pwd`、`ls`、`git status` | 可配置 auto |
| 写文件、安装包、重启服务 | require approval |
| `rm -rf`、磁盘/用户/权限操作 | deny 或强制人工审批 |
| 打开持久 PTY | require approval |

风险分类不能只依赖字符串匹配；第一阶段可以使用命令规则 + policy，后续增加结构化风险标签和人工确认原因。

HITL payload 建议包含：

```json
{
  "hitl_type": "execute_tool",
  "tool": "linux_exec",
  "channel_id": "prod-app-01",
  "display_name": "生产应用 01",
  "remote_user": "deploy",
  "cwd": "/srv/app",
  "command_preview": "systemctl restart app",
  "risk_level": "service_restart",
  "approval_reason": "远程重启生产服务"
}
```

不包含任何 password、private key 或 secret_ref。

## 10. API 设计

### 10.1 Channel 管理

```text
GET    /v1/linux/channels
POST   /v1/linux/channels
GET    /v1/linux/channels/{channel_id}
PATCH  /v1/linux/channels/{channel_id}
DELETE /v1/linux/channels/{channel_id}
POST   /v1/linux/channels/{channel_id}/test
POST   /v1/linux/channels/{channel_id}/open
POST   /v1/linux/channels/{channel_id}/close
GET    /v1/linux/channels/{channel_id}/status
```

### 10.2 Credential 管理

```text
GET    /v1/linux/credentials
POST   /v1/linux/credentials
PATCH  /v1/linux/credentials/{credential_id}
DELETE /v1/linux/credentials/{credential_id}
POST   /v1/linux/credentials/{credential_id}/test
```

GET 只返回：`credential_id`、显示名、认证类型、更新时间和是否可用；禁止返回秘密。

创建凭据时，密码认证可提交以下两种请求之一：

```json
{"auth_type":"password","secret_ref":"env:SSH_PASSWORD"}
```

```json
{"auth_type":"password","secret_value":"本地 SSH 密码"}
```

私钥也可以使用同样的 `secret_value` 字段直接输入，服务端会加密保存。

`secret_value` 支持 `password` 和 `private_key` 认证，响应只返回 `secret_source`（`environment` 或 `direct`），不返回密码、私钥或内部 `secret_ref`。

### 10.3 Agent 绑定

```text
GET    /v1/agents/{agent_id}/linux-channels
PUT    /v1/agents/{agent_id}/linux-channels
PATCH  /v1/agents/{agent_id}/linux-channels/{channel_id}
DELETE /v1/agents/{agent_id}/linux-channels/{channel_id}
GET    /v1/agents/{agent_id}/linux-channels/effective
```

绑定更新应立即影响后续工具注册和执行，但不应强行杀死正在运行的命令；禁用时新命令立即拒绝，运行中的命令按 cancel/graceful shutdown 处理。

### 10.4 运行事件

复用 Node SSE，但新增专用事件：

```text
linux_channel_connecting
linux_channel_opened
linux_channel_closed
linux_channel_error
linux_command_started
linux_command_output
linux_command_exited
linux_command_cancelled
```

事件必须带：`agent_id`、`session_id`、`channel_id`、`tool_call_id`、`seq`。

输出应受 `max_output_bytes` 限制，避免远程命令输出撑爆上下文和 SSE。

## 11. UI 设计

### 11.1 Linux 通道设置页

分为三类页面：

1. **通道列表**：名称、host、port、user、认证类型、启用状态、最近测试结果；
2. **通道编辑**：连接信息、host key、超时、默认 cwd、并发限制；
3. **凭据编辑**：密码/密钥写入，只显示已配置，不回显秘密。

提供“测试连接”按钮，测试结果只展示：

- DNS/网络是否可达；
- SSH host key fingerprint；
- SSH 认证是否成功；
- 远程用户名；
- 默认 shell；
- 远程工作目录是否存在。

不要在测试结果中显示密码、完整环境变量或远程命令输出中的敏感内容。

### 11.2 Agent 绑定页

在 Agent 设置中增加 Linux Channels 面板：

- 选择多个通道；
- 设置默认通道；
- 配置远程 cwd；
- 设置命令允许/拒绝规则；
- 设置自动执行、需要审批或禁止；
- 查看当前连接和最近错误。

## 12. 权限和安全边界

### 12.1 Node API

通道和凭据管理属于高风险管理 API，不能沿用无鉴权的本机便利路径直接对外暴露。

至少需要：

- 默认只监听 `127.0.0.1`；
- 远程访问必须使用 client token、VPN 或 SSH 隧道；
- 修改凭据、绑定生产通道需要 admin/owner 权限；
- GET 不返回秘密；
- DELETE/禁用操作写审计；
- channel_id、credential_id 使用不可猜测 ID 或严格校验；
- 所有路径、host、port、超时和并发参数做服务端校验。

### 12.2 多 Agent 隔离

同一 Node 上的 Agent 绑定不同 channel 时：

- Agent 只看到自己的 effective channel list；
- tool 参数中的 channel_id 必须做绑定校验；
- session 必须绑定 agent_id；
- 一个 Agent 不能读取另一个 Agent 的命令输出或 PTY；
- 不共享 session cwd、env、stdin 和后台进程；
- channel 删除/解绑不能释放其他 Agent 的 session，需按引用计数关闭。

### 12.3 Manage/Workgroup 边界

第一阶段 Linux channel 是 Node-local 资源：

- Manage 只看到非敏感能力摘要或完全看不到 channel；
- Workgroup 任务不能传递原始密码或私钥；
- 如需让 Workgroup 使用远程 Linux，必须在目标 Node 上显式授权 channel binding；
- Manage 下发的只能是 channel ID、策略或不可用 secret 的引用。

## 13. 失败、取消和恢复

统一错误码：

```text
linux_channel_not_found
linux_channel_disabled
linux_channel_not_bound
linux_credential_unavailable
linux_host_key_unknown
linux_host_key_mismatch
linux_connect_timeout
linux_auth_failed
linux_session_limit
linux_command_timeout
linux_command_cancelled
linux_output_limit
linux_remote_exit_nonzero
linux_transport_closed
linux_policy_denied
linux_approval_required
```

行为建议：

- 连接失败不让 Agent 进程崩溃；
- 认证失败不自动无限重试；
- host key mismatch 直接 fail closed；
- command timeout 先关闭 SSH session，再决定是否重建 client；
- Node 重启后不恢复旧 PTY，普通连接按需重建；
- 自动重连只恢复连接池，不重放有副作用的命令；
- 未知副作用不得自动重试。

## 14. 分阶段落地

### Phase 0：契约和安全基础

- 确定 channel、credential、binding、session 数据模型；
- 确定 secret store 接口；
- 增加 host key 校验；
- 增加命令输出限制和审计字段；
- 不开放模型工具，仅提供 API/测试连接。

### Phase 1：无状态远程执行 MVP

- 使用 Go SSH client；
- `linux_channels` CRUD；
- credential password/private-key/agent 三种认证；
- Agent 多通道绑定；
- `linux_list_channels`、`linux_exec`、`linux_channel_status`；
- 复用 Agent policy、HITL、SSE、取消；
- 每条命令独立 SSH session；
- Web UI 配置和测试。

### Phase 2：连接池和远程文件

- SSH client 复用与空闲回收；
- 远程 `read_file`、`write_file`、`list_dir`；
- 统一 WorkspaceProvider；
- 更精确的命令风险分类；
- 指标：连接耗时、命令耗时、失败率、输出截断数。

### Phase 3：持久 PTY

- `linux_open/read/write/resize/close`；
- PTY SSE 输出；
- session 断线状态和显式恢复；
- 会话级并发、超时和资源配额。

### Phase 4：Exec Server 和多执行后端

- 将 Linux provider 内部协议抽象为 Exec Server；
- Local/SSH/Container/Workgroup 共用执行协议；
- Manage 下发非敏感策略和能力；
- 评估向 Codex/其他项目贡献通用协议、测试或适配器。

## 15. 验收标准

### 基本功能

- 可配置至少两个不同 IP、用户和认证方式的 Linux channel；
- 一个 Agent 可绑定多个 channel；
- 不同 Agent 对 channel 的可见性正确隔离；
- Agent 能选择 channel 执行命令并获得 stdout/stderr/exit code；
- 可取消超时或正在运行的命令；
- Node 重启后配置保留，旧 session 不被错误恢复。

### 安全

- GET API、Agent prompt、SSE、tool result、普通日志均不泄露密码和私钥；
- host key mismatch 被拒绝；
- 未绑定 channel 的 tool call 被拒绝；
- 生产写操作默认触发 HITL；
- 不同 Agent 无法读取彼此的命令结果和 session；
- Manage/Workgroup 链路不传输原始凭据。

### 稳定性

- 网络中断不会导致 Node 崩溃；
- 连接超时、认证失败、远程非零退出码有稳定错误码；
- 超大输出被截断并明确标记；
- 不会因为重试而重复执行未知副作用命令；
- 并发达到上限时快速返回可识别错误。

## 16. 关键技术决策补充

### 16.1 SSH 实现

第一阶段推荐使用 Go SSH 客户端实现 Node 内部 Provider，而不是通过 shell 拼接调用系统 `ssh`：

- 统一处理密码、私钥、超时、取消、stdout/stderr 和退出码；
- Linux 与 Windows Node 使用相同代码路径；
- 避免远程命令经过本地 shell 的第二次转义；
- 便于接入 DAgents policy、HITL、审计和指标。

系统 OpenSSH 可以作为后续可选 Provider，用于支持 `ProxyJump`、复杂 `~/.ssh/config`、PKCS#11 或企业已有 SSH 配置。两种 Provider 共享上层 `LinuxChannelProvider` 接口，Agent 工具层不分叉。

### 16.2 命令执行语义

第一阶段的 `linux_exec` 是“一次调用、一次远程命令、一次结果”：

```text
tool call → binding/policy/HITL → SSH client → SSH session
          → bounded stdout/stderr → exit code → audit/event
```

不承诺跨调用保持 cwd、shell 变量、alias、stdin、后台进程组或 PTY 状态。需要这些语义时，必须使用后续的持久 PTY session，不能让 `linux_exec` 隐式模拟。

### 16.3 CWD、环境变量和连接复用

- channel 提供 `default_cwd`，Agent binding 可覆盖，tool 参数只能在允许范围内覆盖；
- 第一阶段不允许 Agent 任意传入环境变量；后续只允许绑定级白名单和 secret 引用；
- 不把 Node 本地环境变量自动传到远端；
- 连接池只复用 SSH transport，不复用 command session；
- 每个命令创建独立 SSH session，结束后关闭；
- credential 更新、host key 变化、transport EOF 或 keepalive 失败时淘汰连接；
- 必须设置空闲超时、最大连接数、每 channel 并发数和 graceful draining。

## 17. API 契约补充

### 17.1 创建通道

```json
{
  "display_name": "生产应用 01",
  "host": "10.10.0.21",
  "port": 22,
  "username": "deploy",
  "credential_id": "cred_prod_deploy",
  "host_key_policy": "pinned",
  "host_key_ref": "SHA256:...",
  "remote_shell": "bash",
  "default_cwd": "/srv/app",
  "command_timeout_ms": 120000,
  "enabled": true
}
```

响应只返回非敏感元数据。密码、私钥、secret_ref 和认证库原文不得出现在响应中。

### 17.2 Agent 绑定

```json
{
  "bindings": [
    {
      "channel_id": "ch_prod_app_01",
      "enabled": true,
      "is_default": true,
      "remote_cwd": "/srv/app",
      "approval_mode": "require_approval",
      "allowed_commands": ["git", "go", "npm", "systemctl status"],
      "denied_patterns": ["rm -rf /", "mkfs", "shutdown"]
    }
  ]
}
```

服务端必须重新解析并校验整个绑定集合，不能只相信客户端提交的默认通道、allowlist 或 channel ID。

### 17.3 直接 API 与模型工具

建议保留以下直接 API：

```text
POST /v1/agents/{agent_id}/linux/exec
```

模型则只能通过 `linux_exec` tool 进入相同的内部执行路径，不能绕过绑定、policy、HITL 和审计。

## 18. 存储、迁移与热更新

建议使用独立 Node 本地数据库：

```text
.runtime/linux_channels.db
```

channel、credential 和 binding 的 migration 独立编号、独立测试。credential 只存 secret reference；备份和导出默认不包含可恢复的明文 secret。

- channel、credential、binding 优先软删除/禁用；
- credential 被引用时禁止直接物理删除；
- credential 更新递增版本并淘汰连接池；
- 修改 channel 只影响新命令，旧命令使用 immutable execution context；
- 禁用 channel 后新命令立即拒绝，运行中命令 graceful draining；
- Node 重启后不恢复旧 PTY，普通命令按需重建连接。

## 19. 审计、指标与诊断

每次远程执行至少记录：

```text
audit_id, agent_id, session_id, tool_call_id, channel_id,
remote_host, remote_user, credential_id, policy_decision,
risk_level, approval_id, command_digest, started_at, finished_at,
exit_code, error_code, output_bytes, truncated
```

默认不记录 password、private key、secret value 和 SSH handshake 原文。普通环境记录脱敏命令或 digest；高审计环境再考虑加密 command payload。

建议指标包括连接耗时/失败率、命令耗时、超时、取消、输出截断、活动 session 数和连接池空闲数。

## 20. 测试和兼容性

### 20.1 测试

- schema migration、binding 校验、secret 泄漏回归；
- password/private-key 认证；
- known_hosts 未知密钥返回指纹、用户确认后 pinned 复测；
- pinned 指纹匹配和指纹不匹配；
- SSH 连接建立与登录凭据校验；
- stdout/stderr/exit code、超时、取消、输出截断；
- session 并发限制、transport EOF、重连和连接池淘汰；
- Agent A 不能读取 Agent B 的 channel session；
- Manage/Workgroup payload 不含原始凭据。

使用临时 SSH server 或测试容器完成集成测试，至少覆盖一个密码用户和一个密钥用户。

### 20.2 平台

- Linux Node：优先 Go 原生 Provider，减少外部命令依赖；
- Windows Node：使用相同 Go Provider，适配 private key；
- 远程 Linux：只要求标准 sshd 和远程 shell，不要求安装 DAgents；
- 远程用户权限由 Linux 本身决定，不把 Node 的 root 权限映射到远程主机。

## 21. 与现有能力的集成

| 现有能力 | 集成方式 |
|---|---|
| Agent Registry | channel binding 按 `agent_id` 解析 |
| Agent Policy | `linux_exec` 纳入 tool policy，再叠加 binding command policy |
| HITL | 复用 `hitl_required` 和 resume |
| SSE | 增加 channel/command 生命周期事件，遵守现有 seq/cursor |
| Session | tool call/result 进入现有会话事实流，连接状态进入运行时事件 |
| Skills | 只能封装工具，不能绕过 channel binding/policy |
| MCP | 后续可包装为 MCP，但底层复用同一 Provider |
| Workgroup | 目标 Node 显式授权 channel，不转发 secret |
| Manage | 后续同步非敏感能力摘要和策略，暂不分发 secret |

## 22. 方案完成后的开发计划入口

后续开发计划应拆成以下工作流：

```text
契约/数据/secret store
        ↓
Provider + 连接生命周期
        ↓
API + Agent binding
        ↓
Policy/HITL/SSE/audit
        ↓
Web UI
        ↓
MVP E2E
        ↓
PTY / 文件 / Exec Server
```

每个阶段需要明确代码目录、migration、API schema、前端页面、测试夹具、验收标准、回滚方式和是否涉及 Manage。开发计划批准前，不新增 Manage secret 分发、持久 PTY 或远程文件同步等跨层能力。

## 23. 关键决策

当前推荐的最小可行方案是：

```text
Node-local channel profile
  + local credential reference
  + per-Agent multi-binding
  + Go SSH provider
  + one-shot linux_exec
  + existing policy/HITL/SSE/audit
  + strict host-key verification
```

不要第一步就实现持久 PTY、远程文件同步、Manage secret 分发和独立 Exec Server。先把“多通道配置—Agent 绑定—安全执行—结果回传—审计”闭环跑通，再逐步扩展执行协议。
