# Bash 工具专项审查（2026-08-17）

## 1. 范围与结论

本报告针对 DAgents 当前 `bash_run`、已有 `linux_exec` 执行后端，和 DeepSeek Harness、OpenAI Codex 的 Shell/Subprocess/Exec 实现进行代码级对照。外部项目以 2026-08-17 的 `master/main` 源码为准；本地验证执行了 `go test ./node/internal/tools`，结果通过。

结论：DAgents 已经有一个可继续演进的执行抽象，尤其是 `ShellProvider`、`Process`、超时、后台任务、Windows Job Object、SSH channel 和执行事件都具备了基础。但 `bash_run` 的资源边界和安全边界仍有四个实质缺口：

1. 输出上限配置没有落到实际采集路径，`bytes.Buffer` 可能在压缩和 tool-result spill 之前无限增长。
2. 本地 Bash 默认继承 Node 的完整进程环境，可能把 `OPENAI_API_KEY`、数据库密码等敏感变量暴露给模型命令。
3. `cwd`/`workspace_root` 主要是词法路径检查，符号链接、junction、bind mount 等可以使执行目录指向根目录外；它不是 OS 级沙箱。
4. Linux channel 的部分绑定策略已经存储并返回，但 Provider 当前只执行 `Enabled`、远程 cwd 和 shell，`AllowedCommands`、`DeniedCommands`、`MaxConcurrency`、`ApprovalMode` 还没有真正进入执行决策。

因此不建议重写 `bash_run` 的模型接口。建议保留现有接口，把“采集、环境、进程生命周期、目标策略”继续下沉到统一执行层，并新增明确的 `EnvironmentPolicy`、`OutputBudget`、`TargetPolicy` 和远程进程终止契约。

## 2. 三方实现对照

| 维度 | DAgents 当前实现 | DeepSeek Harness | OpenAI Codex | 对 DAgents 的启发 |
|---|---|---|---|---|
| Shell 层 | `bash -lc`、PowerShell、cmd；工具与执行准备逻辑仍较紧密 | `tool-bash` 只是 Consumer；`ctx.shell` 负责 Shell，`ctx.subprocess` 负责进程 | Shell handler 负责参数、审批、环境和 runtime；统一执行可落到本地或 exec-server | 保留 `bash_run`，继续把工具参数和执行 Provider 分开 |
| 输出 | 同步/后台 Bash 使用 `bytes.Buffer`；结果后处理才做 sanitize/spill | Subprocess 按 stream 限制内存，超出后保留 tail，并可 spill 完整输出 | `HeadTailBuffer` 保留 head/tail；exec-server 按 `seq` 读取和回放 | 必须在 IO 采集入口限流，不能只在结果生成后截断 |
| 环境 | `req.Env` 为空时 `exec.Cmd.Env=nil`，继承完整 Node 环境 | `scrubbedParentEnv` 删除 credential-shaped 和 Harness 内部变量；显式 env 再覆盖 | `ShellEnvironmentPolicy` 支持 inherit/exclude/include-only/set，并按 environment 生效 | 引入默认 scrub + 显式 allow/set；环境策略必须可审计 |
| 工作目录/隔离 | `workspace_root` 下的词法路径检查；无独立 sandbox 进程 | `ctx.sandbox` 可使用 bwrap、Landlock、Seatbelt、Windows ACL，并对不可用 runner fail closed | Sandbox transform、权限 profile、网络策略可按 execution environment 生效 | `workspace_root` 只能是应用边界，不能对外宣称安全沙箱；逐步接入 OS sandbox |
| 进程生命周期 | `Process` 有 Start/Wait/Terminate；POSIX session、Windows Job Object；后台 job 有 SQLite 状态 | 独立 Subprocess seam 管理 detached tree、SIGTERM→SIGKILL、waitForExit | `process/start/read/write/terminate`，PTY、输出 seq、断线清理和远程环境 | 统一进程协议应支持 read cursor、write、terminate、closed 和 whole-tree wait |
| 交互式执行 | `bash_run` 非 PTY；另有 Terminal/SSH Terminal 抽象 | Bash one-shot 与 persistent Terminal 分开，Terminal 自己管理 PTY/readiness | unified exec 和 exec-server 原生支持 PTY、stdin、signal | 继续区分 `bash_run`、`linux_exec`、`terminal_*`，不要把交互状态塞进一次性命令 |
| 远程执行 | `linux_exec` 每次新建 SSH session，绑定到 Agent/channel | 通过 capability/provider 替换执行世界，另有 E2B subprocess | environment registry + exec-server，远程 transport 有过程协议 | channel 是 execution target，不应只是 command 前缀或连接配置 |

## 3. 已确认的缺陷

### P0：Bash 输出上限没有真正生效

`bash_compress.go` 定义了 `MaxOutputChars`、`MaxOutputCharsStderr` 和 `compressBashStream`，但 `formatShellCompletedOutput` 实际调用的是 `sanitizeBashStream`，没有按上限截断。同步路径使用普通 `bytes.Buffer`，后台 collector 也使用两个普通 `bytes.Buffer`。`toolresult.Package` 的 spill 发生在工具结果已经形成之后，因此只能降低 history 体积，不能防止命令期间的内存增长。

影响包括：

- `yes`、编译日志、日志跟踪等命令可以持续扩大 Node 内存占用；
- 后台任务的输出会一直保存在 `backgroundJob.bashStdout`/`bashStderr`；
- `processEventSink` 仍会逐块向 SSE 层发送输出，慢客户端虽会丢 ephemeral 事件，但不会限制进程内采集。

建议：

- 在 `Process`/Provider 的 stdout、stderr 采集入口增加字节预算；
- 默认保留 head/tail，超出后标记 `truncated`，可选将完整流 spill 到私有目录；
- 输出事件增加单进程速率/字节预算，不能依赖 SSE subscriber 丢包来做资源控制；
- 增加 `yes`、单 chunk 超限、后台超限和 stderr 超限回归测试。

参考实现：[Harness subprocess OutputCollector](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/subprocess/subprocess-local/src/spawn.ts)、[Codex HeadTailBuffer](https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/src/unified_exec/head_tail_buffer.rs)。

### P0：Shell 默认继承 Node 的完整环境

`LocalShellProvider.Start` 只有在 `req.Env` 非空时才设置 `cmd.Env`；Bash 当前没有传入 `req.Env`，因此 Go 会让子进程继承 Node 的完整环境。`local_terminal` 也采用同样的继承语义。对 Node 进程而言，这很可能包括 LLM API key、数据库连接密码、内部服务 token 和部署凭据；模型只需调用 `env`、`printenv` 或读取 `/proc` 即可能看到它们。

建议将环境作为执行策略的一部分：

- 默认继承经过 scrub 的最小环境，例如 `PATH`、`HOME`、locale 和必要代理变量；
- 按名称模式删除 `KEY/PASSWORD/SECRET/TOKEN` 等敏感变量，并删除内部运行时变量；
- 只有显式的、经过策略允许的 `env` 才能重新注入；
- 输出环境策略的摘要和命中规则，不输出值；
- local shell、local terminal、Linux SSH wrapper 和未来 exec-server 使用同一套规则。

参考实现：[Harness scrubbedParentEnv](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/subprocess/subprocess/src/index.ts)、[Codex ShellEnvironmentPolicy](https://raw.githubusercontent.com/openai/codex/main/codex-rs/config/src/shell_environment_policy.rs)。

### P1：`workspace_root` 检查不是安全隔离边界

`resolveRunCWD` 和 `resolvePath` 对字符串规范化后的路径做前缀检查，但没有对目标目录执行 `EvalSymlinks`，也没有使用 OS sandbox。`workspace_root/link-to-outside` 这样的符号链接或 Windows junction 可以让 Shell 在根目录外工作。即使增加 realpath 检查，检查和真正 `exec` 之间仍有 TOCTOU 问题。

建议分层处理：

1. 立即将 `filepath.Rel`、canonical root/target 和 Windows 大小写/UNC/junction 测试补齐，减少明显误判；
2. 把这类检查定义为应用层 workspace boundary；
3. 对不可信 Agent 增加 bwrap/Landlock/Windows restricted token 等 OS 级 sandbox；
4. sandbox 不可用时明确 fail closed 或显式降级为“无 sandbox 的受审批执行”，不要静默冒充隔离。

参考实现：[Harness sandbox-local](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/sandbox/sandbox-local/src/index.ts)、[Codex process sandbox preparation](https://raw.githubusercontent.com/openai/codex/main/codex-rs/exec-server/src/process_sandbox.rs)。

### P1：Linux channel 的绑定策略存在“已存储但未执行”

`LinuxChannelBinding` 包含 `AllowedCommands`、`DeniedCommands`、`MaxConcurrency` 和 `ApprovalMode`，数据库和 API 也会保存这些字段，但 `LinuxShellProvider.Start` 当前只使用绑定的 `Enabled`、`RemoteCWD` 和 `Shell`。全局 policy 的 `linux_exec` 决策也没有把 `channel_id` 纳入命令策略键。

这会造成 UI/配置给用户一种“channel 已受限”的感觉，但实际执行仍可能只经过全局 `linux_exec` 审批。建议：

- 在执行前解析有效绑定，先执行 channel enabled 和 Agent ownership；
- `DeniedCommands` 先行匹配，命中立即拒绝；`AllowedCommands` 非空时必须命中，否则拒绝或审批；
- `ApprovalMode` 与全局 policy 取更严格结果；
- 用 Agent+channel 的 semaphore 执行 `MaxConcurrency`；
- 审计事件记录 channel id、binding version、匹配结果和 approval id；
- 不要只依赖 Shell 字符串 root 判断作为安全边界。

### P1：远程 SSH 的 Terminate 不是远程进程树终止

`linuxProcess.Terminate` 当前最终调用 `Close`，关闭 SSH session/client。关闭 SSH channel 通常能结束普通前台命令，但不能证明所有远程子进程已经退出；`nohup`、daemonize、双重 fork 或继承文件描述符的进程可能继续运行。当前 `linux_exec` 也明确是一次性 session，不提供远程 job 恢复或远程进程查询。

建议在允许远程后台或高风险命令前，增加远程 helper/包装协议：以远程 process group/session 启动，记录远程 pid/标识，超时时执行组级终止并等待确认；无法确认时将结果标记为 `termination_unknown`，不要只返回普通 timeout。

## 4. 重要的架构优化项

> 本报告是 2026-08-17 的研究快照，不是当前 API 契约。文件名和实现已随清理变更；现行行为以 `node/internal/tools/`、工具 schema 和测试为准。

### 4.1 默认使用非 login shell

DAgents 本地 Bash 使用 `bash -lc`，Harness 默认是 `bash -c`，Codex 也把 login shell 作为受配置控制的选项。login profile 会引入用户自定义 PATH、alias、函数、网络初始化、输出和副作用，降低 Agent 执行的可重复性，也会放大环境泄露问题。

建议默认改为 `bash -c`，增加显式 `login: true` 或等价内部选项，并使其进入审批/审计字段。

### 4.2 把命令 policy 定义为审批提示，不定义为沙箱

当前 `ParseCommandRoots` 能拆分常见的 `;`、`&&`、`||`、`|`，再提取 root command。但 Bash 可以通过变量、函数、命令替换、嵌套 `bash -c`、解释器、编码脚本等方式改变真实执行行为。因此“root command allowlist”适合做风险分类和 HITL 提示，不适合独立承担强制安全边界。

建议对以下场景默认提高风险等级或要求审批：嵌套 shell、解释器、命令替换、重定向、后台符号、网络命令、权限变更和无法完整解析的语法；最终边界仍由 OS 用户、sandbox、channel policy 和远端账户权限提供。

### 4.3 统一本地与远程进程协议

DAgents 已有 `ExecRequest`/`Process`，方向是正确的；但建议继续对齐 Codex exec-server 的最小协议：

```text
start(target, argv|shell, cwd, env_policy, tty, stdin_mode, limits)
  -> process_id
read(process_id, after_seq, max_bytes, wait_ms)
write(process_id, bytes)
signal/terminate(process_id)
  -> exited -> closed
```

其中 `stdout`、`stderr`、`pty` 应明确区分；事件使用每进程序号；读取支持 cursor 和 bounded replay；`terminate` 的语义是 whole-tree/whole-session，而不是只关闭一个本地句柄或 SSH channel。

### 4.4 对后台任务增加资源治理

DAgents 已有后台任务 SQLite 状态，这是优点，但当前应补充每 Agent、每 channel 和每 Node 的：并发数、CPU/内存/磁盘/输出预算、最大保留时间、Node 重启后的 unknown 状态，以及孤儿进程回收。Codex 的 unified exec 有进程数量上限和进程 store；Harness 的 Subprocess seam 将树生命周期和 teardown 统一放在基础设施层。

## 5. 建议落地顺序

### 第一阶段：先修真实风险

1. 把 Bash stdout/stderr 改成有上限的采集器；修正 `formatShellCompletedOutput` 使用上限；
2. 默认环境 scrub，补充显式 env allow/set；
3. 增加 symlink/junction/TOCTOU 的测试与文档，明确 `workspace_root` 不是 sandbox；
4. 为 channel binding 的 command policy、approval mode 和 concurrency 加执行路径。

### 第二阶段：完善远程生命周期

1. 远程命令 wrapper/process-group/termination confirmation；
2. channel 级 `read/status/cancel` 和 `termination_unknown`；
3. 远程后台 job 是否支持恢复，单独定义协议，不复用本地 job 的假设。

### 第三阶段：执行后端演进

1. 抽出统一 `SubprocessProvider` 和 `TerminalProvider`；
2. 为本地和 SSH 复用 process event、cursor read、output budget、audit context；
3. 在可选能力中接入 OS sandbox；
4. 保持 `bash_run`、`linux_exec` 的模型参数兼容，把新协议隐藏在 Node 内部。

## 6. 代码索引

- DAgents：[bash_run_tool.go](../../node/internal/tools/bash_run_tool.go)、[bash_runner.go](../../node/internal/tools/bash_runner.go)、[bash_compress.go](../../node/internal/tools/bash_compress.go)、[execution.go](../../node/internal/tools/execution.go)、[shell_execution.go](../../node/internal/tools/shell_execution.go)、[linux_shell_provider.go](../../node/internal/tools/linux_shell_provider.go)。
- Harness：[tool-bash](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/shell/tool-bash/src/index.ts)、[bash-local](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/shell/bash-local/src/index.ts)、[subprocess types](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/subprocess/subprocess/src/types.ts)、[terminal-bash](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/terminal/terminal-bash/src/session.ts)。
- Codex：[shell handler](https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/src/tools/handlers/shell.rs)、[unified exec process](https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/src/unified_exec/process.rs)、[exec-server protocol](https://raw.githubusercontent.com/openai/codex/main/codex-rs/exec-server/README.md)、[environment policy](https://raw.githubusercontent.com/openai/codex/main/codex-rs/config/src/shell_environment_policy.rs)。
