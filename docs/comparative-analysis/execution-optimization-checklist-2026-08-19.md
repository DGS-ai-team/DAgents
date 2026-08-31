# DAgents 执行与运行时优化 Checklist

> 基于 `baseline-2026-08.md`、`bash-tool-review-2026-08-17.md`、`delta-2026-08-17.md` 和 `runtime-snapshot-cache-analysis-2026-08-19.md`，结合当前代码复核。
>
> 状态：`[x]` 已完成，`[~]` 部分完成，`[ ]` 未完成。

## 一、运行时一致性与上下文缓存

- [x] 为每个 Turn 固定 `ModelContextSnapshot`，保持 system prompt 和 tools schema 稳定。
- [x] 将最新 policy 检查从模型上下文快照中分离为 `ExecutionGuard`。
- [x] 配置变化在活动 Turn 中延迟到 idle 边界，并发布 `runtime/config-changed`。
- [x] Prompt sidecar 保存后刷新当前 runtime reader。
- [x] `long_term_scope` 写回 Agent snapshot，并在下一 Turn 使用。
- [x] 压缩侧车复用当前 Turn 的 system/tools 前缀。
- [x] 记录 runtime、prompt、tool digest 以及 prompt cache hit/miss 指标。
- [x] Skill、MCP、工具清单做稳定排序和规范化序列化。
- [x] Runtime 构建输入在 API 层准备完成后，由 Manager 以 replace/swap 事务替换；新 runtime 失败时保留 last-good runtime。
- [x] 增加 runtime replace 失败保留旧 runtime、成功交换并保留内存历史的回归测试。
- [~] Turn 活跃期间配置变化的延迟与重启恢复已有基础覆盖，仍需补充“替换请求与 idle 边界并发”的专项测试。

## 二、Shell 与执行安全

- [x] `linux_exec`、`bash_run` 同步和后台采集均在采集边界使用有界 buffer，并在结果/telemetry 中保留截断标记。
- [~] 已完成本地 Shell、Terminal 的默认敏感环境变量 scrub，并接入统一 `EnvironmentPolicy` 的 inherit/scrub/allow/set 模式；MCP stdio 已默认 scrub 且保留显式 env/env_refs，跨包策略复用和日志/Prompt/SSE 脱敏仍可继续统一。
- [~] 统一 `OutputBudget`：已抽取可复用的 stdout/stderr 字节预算对象，支持 UTF-8 安全截断及可选 head/tail；bash 的 YAML/设置项已接入，Linux/其他 provider 的统一策略仍待补齐。
- [~] 增加 `EnvironmentPolicy`，区分 inherit、scrub、allow 和 explicit set；本地 Shell 与 Terminal 已接入，MCP stdio 已有等价的默认 scrub，跨包共享类型及审计脱敏仍待统一。
- [~] Agent `workspace_root` 有应用层路径检查，但不是 OS sandbox；Node 管理目录使用独立 `runtime_root`。
- [ ] 补齐 symlink、junction、UNC 和 TOCTOU 测试，并在 UI/文档中明确“无 OS sandbox”状态。
- [ ] 增加 Linux bwrap/Landlock、Windows restricted token 等可选 sandbox provider。
- [ ] 默认使用非 login shell，并提供显式 login shell 选项。

## 三、Linux channel 与远程执行

- [x] 已提供基础 channel test 接口和连接阶段错误信息。
- [x] channel test 已返回配置、凭据、认证、host key、DNS、TCP、SSH handshake、session、command 等结构化阶段结果。
- [x] 失败阶段带有稳定错误码，保留 `available/message` 兼容字段。
- [~] channel 的 allow/deny/concurrency 已进入 Linux provider 执行决策；`linux_exec`、`terminal_open` 和 file transfer 已在排队前与 HITL 合并并在 SSH/SFTP/PTY 建连前二次校验。
- [~] 执行时已应用 channel binding 的命令规则和 `deny` 模式；Agent policy、远端环境 policy 的统一“更严格结果”合并仍需补齐。
- [~] 已增加 Agent/channel 级 semaphore 和规则拒绝；规则命中与 approval reason 已进入统一执行前置链，细粒度策略来源审计仍可继续增强。
- [~] 本地和 SSH 已共用 `ExecutionContext`、`Process` 和 `ShellProvider`；生命周期审计已统一记录命令摘要与输出字节数，仍缺少统一的远程 read/status/cancel 契约。
- [~] 远程命令已有 provider-neutral process/session 句柄、`setsid` 进程组 wrapper 和统一 cancel 入口；Linux 终止会通过短期 PID 标识重连确认并返回 `confirmed`/`force_terminated`/`not_running`，无 PID 标识或确认通道失败时仍返回 `termination_status: unknown`。
- [~] 后台任务已有 status/cancel、进程绑定和 async 回灌；Node 启动时会把所有遗留 running 任务原子标记为 unknown 并记录恢复原因，Linux SSH 后台任务已支持 token/PID 文件校验后的主动回收，更广泛的远端 provider 仍待补齐。

## 四、可观测性与会话可靠性

- [x] 已有 runtime/config、memory、MCP catalog 事件和上下文指标。
- [~] 工具执行事件已带 Agent/Session/Turn/ToolCall 关联信息，channel、approval 和 risk 字段仍需统一补齐。
- [~] 建立统一 `ExecutionContext` 审计结构，已贯穿 policy、HITL、provider、生命周期审计和 SSE telemetry；输出截断原因及远程任务状态仍待补齐。
- [ ] 将远程输出/诊断 telemetry 与持久会话正文分离。
- [ ] 为大型历史、后台任务和恢复场景增加分页/增量读取。
- [ ] 增加 Node doctor：存储、网络、SSH、代理、凭据、能力降级和 sandbox 状态。

## 五、此前 UI P1/P2 事项

- [x] 连续工具调用按组渲染，减少重复气泡。
- [x] 消息离开底部后提供“直达底部”按钮，并显示未读数量。
- [x] Markdown 代码块提供复制、下载操作。
- [x] 流式展示使用 requestAnimationFrame 合帧、`v-memo` 和有限渲染窗口，降低高频重绘。
- [~] 当前已有 180 项渲染窗口和向上加载机制，但还不是完整的高度感知虚拟列表。
- [~] Markdown 流式预览已支持基础渲染；复杂代码块、超长文本和增量解析仍需专门验收。
- [~] 窄屏输入布局和键盘/无障碍语义已有基础处理，仍需在移动尺寸和键盘-only 流程补回归测试。

## 六、实施顺序

### 当前批次：执行安全基线（已完成首版）

- [x] Bash/Terminal 输出有界采集。
- [~] 统一 `OutputBudget` 首版已接入 Bash 与 Linux exec；head/tail 和 provider 级配置待补齐。
- [x] Shell 环境变量 scrub。
- [x] 回归测试：已覆盖 `yes`、stderr 洪水、后台任务有界采集、截断标记、head/tail 预算和敏感环境变量。

### 下一批次：Linux channel 一致性

- [x] Runtime 原子替换。
- [x] Linux channel 分阶段诊断。
- [~] channel policy 实际执行已完成首版，三类远程入口的审批前置合并与命令摘要/输出字节数审计已完成；截断标记、策略来源和远程任务协议仍待补齐。

### 后续批次：远程执行协议

- [~] remote read/status/cancel 基础契约已有 terminal 与 background job 实现；Linux exec 已补充 PID 标识重连确认，terminal terminate 也返回统一的 `termination_status`，其他远端 provider 的统一确认语义仍待补齐。
- [~] 后台任务恢复首版已完成：启动时统一回收 running 状态为 unknown；Linux SSH 已增加 token/PID 文件校验的孤儿回收，其他远端 provider 仍待补齐。
- [ ] Local/SSH/Container/Exec Server provider 统一协议。

## 七、暂不立即实施

- 不重写 `bash_run` 的模型参数接口。
- 不引入通用 `mcp_call` dispatcher 来规避缓存失效。
- 不把 `workspace_root` 的词法路径检查宣传为安全 sandbox。
- 不在尚未稳定 policy、HITL、ExecutionContext 之前引入独立 Guardian 服务。
