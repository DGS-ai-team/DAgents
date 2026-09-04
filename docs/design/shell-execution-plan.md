# Bash 与 Shell 执行层设计方案（历史记录）

> **文档性质**：历史设计记录，不是当前 API 契约。当前实现以 `terminal_*` 工具、`node/internal/tools/terminal_*` 和内置工具参考为准；本文中的 `linux_exec`、后台 job 及迁移兼容描述不得作为新代码实现依据。
>
> 状态：Phase A-D 已实现；后续演进以现行 Terminal/Linux channel 代码为准。
>
> 目标：抽象统一的本地/远程 Shell 执行层，为 Linux channel、PTY、容器和后续 Exec Server 提供共同基础。

## 1. 结论先行

不建议把 `bash_run` 直接改成同时支持本地和远程：

```text
bash_run   → 本地 Node 一次性执行
terminal_* → 本地或 Linux channel 的持久终端
linux_exec → 旧 Agent 快照的兼容入口
```

但内部共用同一个执行抽象：

```text
Tool Layer
  ├─ bash_run
  └─ linux_exec
        ↓
Policy / HITL / Audit
        ↓
Execution Request
        ↓
Executor Provider
  ├─ LocalShellProvider
  ├─ SSHShellProvider
  ├─ ContainerShellProvider
  └─ ExecServerProvider
```

这样既保持用户和已有 Agent 的兼容性，也可以逐步吸收 DeepSeek Harness 和 Codex 的优点。

## 2. DAgents 现有 Bash 能力

当前 `bash_run` 已经具备不少可复用基础：

- Linux 下通过 `bash -lc` 执行；
- Windows 下支持 `cmd`/PowerShell；
- `cwd` 受 Agent `workspace_root` 限制；
- 有超时、取消和进程树终止；旧后台任务仅由兼容层保留；
- 有 stdout/stderr 捕获和输出压缩；
- 通过 Agent policy 和 shell policy 进行审批；
- 已接入 SSE、HITL；旧后台 job 查询和取消不再进入当前模型工具目录。

因此后续重点不是重写 Bash 工具，而是把现有执行逻辑从 Tool Registry 中抽出为可替换 Provider。

## 3. DeepSeek Harness 可借鉴的地方

DeepSeek Harness 的架构文档把 Shell、Subprocess、Terminal 和 Sandbox 分为不同 capability seam：

```text
ctx.shell
ctx.subprocess
ctx.terminals
ctx.sandbox
```

并允许不同 Provider 替换执行世界。对 DAgents 的启发主要在抽象边界，而不是具体 Shell 命令实现。

### 3.1 Shell、Subprocess、Terminal 分层

建议 DAgents 也区分：

| 层 | 责任 |
|---|---|
| Shell | 解析/启动 `bash -lc`、PowerShell 或远程 shell |
| Subprocess | 进程启动、等待、退出码、取消、进程树 |
| Terminal | PTY、stdin、输出流、resize、交互式状态 |
| Sandbox | 执行前路径、权限、环境和隔离策略 |
| Tool | 面向模型的参数和结果格式 |

当前 `bash_run` 把这些责任部分集中在工具实现中，后续可以拆出内部接口。

### 3.2 Capability 和 Agent 绑定

Harness 允许不同 profile/agent 组合不同能力。DAgents 可以借鉴为：

```text
Agent effective capabilities
  ├─ local bash
  ├─ linux channel: devbox
  ├─ linux channel: staging
  └─ terminal/pty
```

但 DAgents 需要比 Harness 更明确地叠加企业 policy：

```text
全局能力
  ∩ Agent 绑定
  ∩ Tool policy
  ∩ Shell/command policy
  ∩ Workspace/sandbox policy
```

### 3.3 Tool Pipeline

Harness 的事件/能力管线适合对应到 DAgents 的 hook：

```text
tool.before_each
  → resolve capability
  → policy decision
  → approval
  → executor
  → tool.after_each
  → session/audit event
```

以后增加 command risk、重复命令检测、命令重写、审计和配额时，不应继续在 `processToolCalls` 里增加特殊分支。

### 3.4 Session Event 事实源

Harness 将模型可见内容和工具执行记录纳入 append-only SessionEvent。DAgents 可以借鉴这一点，区分：

- Durable：`tool_call`、`tool_result`、`approval`、`command_started`、`command_exited`；
- Runtime：token delta、PTY output、heartbeat、连接状态、队列变化。

恢复、审计和上下文投影依赖 Durable 事件；实时 UI 可以消费 Runtime 事件。

## 4. Codex 可借鉴的地方

Codex 的 `exec-server` 更适合参考 Shell 执行的协议和生命周期。它将进程管理抽象成可复用服务，支持 `process/start`、`process/read`、`process/write`、`process/terminate`，并通过 PTY 和异步输出事件处理交互式进程。

### 4.1 进程请求使用结构化字段

不要只传一个 command 字符串，内部请求应至少包含：

```go
type ExecRequest struct {
    Target          ExecutionTarget
    ShellType       string
    Command         string
    Argv            []string
    CWD             string
    Env             map[string]string
    TTY             bool
    PipeStdin       bool
    Timeout         time.Duration
    MaxOutputBytes  int
}
```

其中：

- `Command` 保留 Shell pipeline、重定向和脚本能力；
- `Argv` 为未来不经过 Shell 的安全执行预留；
- `CWD`、`Env`、`TTY` 和 timeout 不应藏在命令字符串中；
- `Target` 区分本地、SSH、容器和 Exec Server。

### 4.2 Process Handle 生命周期

统一抽象：

```go
type Process interface {
    ID() string
    Read(ctx context.Context, afterSeq uint64, maxBytes int) ([]OutputChunk, error)
    Write(ctx context.Context, data []byte) error
    Wait(ctx context.Context) (ExitStatus, error)
    Terminate(ctx context.Context) error
    Close() error
}
```

第一阶段本地 `bash_run` 可以继续同步等待；内部使用 Process 接口后，后台任务、远程命令和未来 PTY 可以共用生命周期。

### 4.3 输出和序列号

统一输出模型：

```text
process/output
  process_id
  seq
  stream: stdout | stderr | pty
  chunk

process/exited
  process_id
  seq
  exit_code
  signal
  truncated
```

要求：

- stdout、stderr 和 PTY 不混淆；
- 每个 process 有单调 seq；
- 有最大输出字节数；
- 截断必须显式标记；
- SSE 重连可以从 cursor 继续；
- 结果不能因为输出过大阻塞整个 Agent。

### 4.4 断线与清理

借鉴 Codex 的行为：执行服务连接关闭时清理属于该连接的受管进程。DAgents 应区分：

- 同步 `bash_run`：Client/turn 取消时终止本地进程树；
- 远程 `linux_exec`：默认关闭远程 SSH session，并尝试终止远程进程；
- 显式后台任务：进入 Job Registry，由 `job_status/job_tail/job_cancel` 管理；
- 持久 PTY：只有显式 `linux_close` 或 session 过期时关闭。

未知副作用命令不允许因为网络重连而自动重放。

### 4.5 Sandbox 与执行层分离

Codex 的重要启发是：sandbox 不应由 Bash 工具自己决定。建议：

```text
ExecRequest
  ↓
SandboxProvider.Prepare(request)
  ↓
ExecutorProvider.Start(request)
```

本地执行可以使用 Agent `workspace_root`、进程限制和后续 Landlock/bwrap；远程执行使用 channel 的远程 cwd、用户权限和 command policy；容器执行使用容器挂载和网络策略。Node 管理文件使用独立的 `runtime_root`，不作为 Agent 工具相对路径基准。

## 5. DAgents 推荐内部接口

### 5.1 执行目标

```go
type ExecutionTarget struct {
    Kind   string // local | linux_channel | container | exec_server
    ID     string // channel_id/container_id/server_id
}
```

### 5.2 Provider

```go
type ShellProvider interface {
    Start(ctx context.Context, req ExecRequest) (Process, error)
    Test(ctx context.Context, target ExecutionTarget) (TargetStatus, error)
}
```

Provider 不负责：

- 决定 Agent 是否有权使用 target；
- 决定是否需要 HITL；
- 生成模型工具描述；
- 写入会话历史。

这些责任由 Tool/Policy/Session 层负责。

### 5.3 执行上下文

```go
type ExecutionContext struct {
    AgentID        string
    SessionID      string
    TurnID         string
    ToolCallID     string
    Target         ExecutionTarget
    PolicyDecision string
    ApprovalID     string
    RiskLevel      string
}
```

所有本地和远程执行都要携带该上下文，保证审计和取消可以统一处理。

## 6. `bash_run` 与 `linux_exec` 的边界

### 6.1 `bash_run`

保持现有产品语义：

- 默认本地执行；
- `shell_type` 继续支持 bash/cmd/powershell；
- `cwd` 继续受本地 Agent `workspace_root` 限制；
- 保留 timeout、background、cancel、output compression；
- 继续使用当前 `bash_run` policy 和 shell policy；
- 不新增 `channel_id` 参数。

不建议通过增加可选 `channel_id` 把本地 Bash 变成远程万能工具，因为这样会：

- 让模型难以理解执行位置；
- 扩大原有工具的权限范围；
- 破坏旧 Agent 的工具语义；
- 让审批界面同时承载本地和远程风险；
- 增加错误重试和审计歧义。

### 6.2 `linux_exec`

明确面向远程 Linux：

- 必须指定或选择已绑定 channel；
- 默认不暴露任意远程环境变量；
- 远程 cwd 使用 channel/binding 规则；
- 复用同一 Policy/HITL/Session/Audit 管线；
- 结果增加 channel、remote user、remote host 摘要；
- 默认使用独立命令 session；
- 后续可升级到 PTY 和远程文件 provider。

### 6.3 后续的通用 Tool

内部可以有统一的 `Execute` 服务，但模型层不一定马上暴露：

```text
internal ExecutionService
  ├─ ExecuteLocalShell
  ├─ ExecuteLinuxChannel
  └─ OpenTerminal
```

当用户真的需要跨执行目标统一操作时，再考虑面向模型增加 `exec` 工具；在此之前保持显式工具名称更安全。

## 7. Policy 设计

建议采用四层合取：

```text
Tool policy
  ∩ shell/command policy
  ∩ target/channel binding
  ∩ sandbox/workspace policy
```

决策流程：

```text
解析 tool call
  → 验证 target
  → 验证 Agent binding
  → 计算风险
  → tool.before_each
  → HITL / deny / auto
  → SandboxProvider
  → ShellProvider
```

同一 command 在本地和远程不应共享完全相同的默认风险：

| 操作 | 本地默认 | 远程默认 |
|---|---|---|
| `pwd`、`ls`、`git status` | 可 auto | 可配置 auto，生产建议审批 |
| 写工作区文件 | policy/rule | require approval |
| 安装包、改服务 | require approval | require approval |
| 用户、权限、磁盘、关机 | deny/强审批 | deny/强审批 |
| 持久 PTY | 后续按需 | require approval |

命令风险判断可以从规则开始，但不能把“字符串包含某个词”作为唯一安全边界。最终权限仍由 OS 用户、sandbox、工作区和 provider 限制。

## 8. 对现有实现的改造顺序

### Phase A：不改变外部行为

- 从 `bash_run` 中抽出 `ExecRequest`；
- 将本地 `exec.Cmd` 封装为 `LocalProcess`；
- 保留现有 tool 参数、timeout、cancel 和结果格式；旧后台 job 只保留历史 wire 兼容；
- 现有测试全部继续通过。

### Phase B：统一事件和 Process 生命周期

- 引入 Process ID、seq、stdout/stderr chunk、exit status；
- 让同步和后台任务共用 Process；
- 统一取消、超时、进程树终止和输出上限；
- 将执行事件接入现有 SSE/审计。

### Phase C：加入 Linux channel

- 实现 `SSHShellProvider`；
- 新增 `linux_exec`，不改变 `bash_run`；
- 复用 Tool Hook/HITL/Job Registry；
- 增加 channel/session/remote error 事件。

### Phase D：PTY

- 增加 Terminal Provider；
- `open/read/write/resize/close`；
- 本地和 SSH PTY 使用同一 Process/Terminal 契约；
- 明确断线清理和恢复语义。

### Phase E：Exec Server 和容器

- 将 Process 协议外置为 WebSocket/JSON-RPC；
- Local/SSH/Container/Workgroup 使用同一执行协议；
- 评估是否需要独立远程 server 和更强 sandbox。

## 9. 最终参考关系

| DAgents 设计点 | 主要参考 | 借鉴内容 |
|---|---|---|
| Shell/Process/Terminal/Sandbox 分层 | DeepSeek Harness | Capability seam、Provider、事件扩展点 |
| Agent 能力组合和工具管线 | DeepSeek Harness | profile/bundle/patch、scoped capability、tool pipeline |
| 结构化进程控制 | Codex | exec-server、process start/read/write/terminate |
| PTY 和输出流 | Codex | tty、异步 output、seq、断线清理 |
| SSH 远程执行 | Codex | SSH transport 与执行服务分离 |
| 企业审批和多 Agent policy | DAgents | Agent policy、HITL、SSE、Workgroup 边界 |

## 10. 参考资料

- [DeepSeek Harness 架构文档](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/docs/architecture.md)
- [Codex exec-server 文档](https://raw.githubusercontent.com/openai/codex/main/codex-rs/exec-server/README.md)
- [Codex SSH 执行脚本](https://raw.githubusercontent.com/openai/codex/main/scripts/start-codex-exec.sh)
> Implementation update (2026-08-17): local native PTY, SSH PTY, single-connection WebSocket transport, bounded replay, and 30-second reconnect grace are implemented; cross-Node restart recovery remains pending.
