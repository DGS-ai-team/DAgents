# Agent 质量与效果验证报告（2026-08-22）

## 1. 验证目的与边界

本轮针对合并后的最新源码，使用真实 LLM、真实工具链和隔离 Node 验证 Agent 在运维、日常办公、多步骤执行、失败处理、异步回调、交互终端和 MCP 场景下的行为，并记录影响下一阶段质量提升的缺口。

验证在独立 Node 实例中进行，主工作区正在运行的 Node 实例未停止、未修改。验证覆盖运行时行为，不等同于完整的浏览器 UI 回归；UI 仍应按既有清单补测。

## 2. 环境

| 项目 | 实际值 |
|---|---|
| 源码基线 | `75940f8f`（合并 PR #254 后） |
| 隔离 Node | `0.9.17`，旧实例 `127.0.0.1:18766`；最新源码实例 `127.0.0.1:18769` |
| 主 Node | `127.0.0.1:18765`，本轮未干扰 |
| 操作系统 | Windows amd64，另有可用本机 WSL |
| LLM | 已配置的真实 Mimo profile，非 mock |
| SSH | 已配置并通过 host key pinning 的 Linux 通道 |
| Draw.io MCP | `@drawio/mcp@1.5.0` |

## 3. 场景结果

| 场景 | 结果 | 关键证据 |
|---|---|---|
| 本地 Bash 只读命令 | 通过 | `status=SUCCEEDED`、`exit_code=0`、stdout 非空且未截断；模型正确报告标记和目录项 |
| Linux SSH 运维命令 | 通过 | SSH preflight 的 config/credential/auth/host key/DNS/TCP/handshake/session/shell/command 全部通过；`linux_exec` 返回 `exit=0`、`stdout_bytes=1955`、`output_truncated=false`，包含 `REMOTE_MARKER`、`Linux`、`/` 和完整 `ls -la` |
| Linux 远程环境告警 | 已正确暴露 | stderr 返回 `/home/devuser` 不存在的 `Could not chdir...`；stdout 仍可读取，模型没有把 stderr 当成 stdout 或掩盖告警 |
| 工具失败恢复 | 通过 | 不存在路径的 `bash_run` 返回 `status=FAILED`、`exit_code=1`、`stdout_bytes=0`；模型明确说明路径不存在、检查未完成，没有伪造成功 |
| 日常办公/数据整理 | 通过（文件型） | `write_file` 写入 CSV 和 Markdown，随后 `read_file` 回读；总额 2500、最高项 hardware=1200 均有文件证据 |
| 真实 Office 文档 | 环境阻塞 | 当前 Windows 未安装 OfficeCLI/LibreOffice，因此 DOCX/XLSX/PPTX 的真实生成和渲染未验证，不应把文件型办公通过等同于 Office 文档能力通过 |
| `terminal_open` + WSL | 通过 | `terminal_config_list` 返回并实际使用 `local-wsl`；`terminal_open` 使用 `shell=wsl`、`rows=24`、`cols=100`；`terminal_read` 返回 `TERMINAL_OPEN_MARKER`、`output_empty=false`；随后终止会话 |
| shell 不匹配防护 | 暴露问题但行为可解释 | 首次 WSL 测试中模型发送 PowerShell `Write-Output`，WSL 返回 `command not found`；模型根据结果说明失败，没有误报成功。改用 POSIX `printf` 后成功 |
| 异步 Bash 回调 | 通过 | 首次返回 `status=RUNNING` 和 `job_id`，未轮询；消息队列自动回灌最终 `status=succeeded`、`exit_code=0`、stdout 包含 `ASYNC_CALLBACK_MARKER`，turn 正常收敛 |
| Draw.io MCP Mermaid | 通过（带启动规避参数） | MCP 初始化后暴露 3 个 allowlisted 工具；模型实际调用 `mcp__drawio__open_drawio_mermaid`，返回 Draw.io URL 和“已在默认浏览器打开”的工具事实，最终报告与事实一致 |
| Web UI 单元回归 | 通过 | Vitest 47 个文件、284 个测试全部通过，覆盖 hydrate、turn 状态、SSE、工具任务、取消、终端会话和工作台辅助逻辑等关键模块 |
| Terminal 工作台导航与输入模式 | 通过（隔离最新构建） | 内置浏览器在 `127.0.0.1:18769` 确认新终端目标选择、真实 WebSocket、远程 Linux 输入、Agent/Terminal 模式切换、Agent 面板折叠、返回消息页后恢复同一 session、MCP 状态事件刷新和真实 Agent turn |
| Terminal Session 文件传输 | 通过（隔离最新构建） | `terminal_upload` 与 `terminal_download` 均完成，8,192 字节两端 SHA-256 一致；错误本地路径产生明确 failed 记录 |

## 4. 已发现的实现与效果问题

### 4.1 P1：MCP 启动诊断与全局健康状态（首轮已修复）

本节前半记录的是首轮验证时的缺口；当前源码已经补齐有界 stdio stderr、进程退出码、initialize/list_tools/catalog/call 阶段、failure kind、retryable 和 Node 级状态事件。前端状态栏订阅全局 `mcp/status-changed`，30 秒查询仅作为恢复兜底。本轮用不存在的 stdio 命令实测：状态栏从“未配置”即时变为“存在异常”，展开后显示 `initialize · transport · 可重试` 及具体错误，未把异常显示为健康。

用户给出的配置使用 `npx -y @drawio/mcp`。在当前 Windows Node 24.18.0 环境，`@drawio/mcp@1.5.0` 的 postinstall 子进程以 Windows 崩溃码 `3221226505` 退出，DAgents 首次只表现为 `context deadline exceeded` / initialize 失败，缺少子进程 stderr、退出码和“安装脚本崩溃”的分类信息。

验证时将命令归一化为 `npx.cmd --ignore-scripts -y @drawio/mcp` 后初始化成功。该参数是本轮环境规避手段，不应由产品无条件替用户关闭所有 npm 生命周期脚本。

建议：MCP 启动器捕获 stdout/stderr、退出码、信号和启动阶段；将 install/postinstall、进程启动、initialize、tools/list 分开报错；Windows 自动解析 `npx.cmd`；支持可配置的包版本和启动参数；刷新接口异步化并在设置页展示可操作诊断。

#### 4.1.1 MCP 状态栏方案

建议在现有底部状态栏增加 Node 级 MCP 状态图标，不放入消息流，也不进入模型上下文。折叠状态至少区分：未配置、全部健康、检查中、存在异常；同时使用图形、颜色和无障碍文本，不能只依赖颜色。

聚合状态建议按启用服务计算：

- 没有启用服务：中性“未配置”，不视为异常。
- 所有启用服务为 `ready`：绿色“全部健康”。
- 任一服务正在刷新或初始化：蓝色“检查中”。
- 任一服务为 `error`/`offline`：黄色或红色“存在异常”。
- `disabled` 是用户主动配置，不应计入异常。
- `ready` 但没有启用工具，表示连接健康、能力未暴露，不应直接判定为服务故障。

点击后展开服务列表，展示服务名、transport、状态、最近检查时间、工具数量、已启用工具数量和脱敏后的最近错误，并提供重试/刷新及跳转 MCP 设置。服务状态应以 MCP Manager 的权威状态为准，而不是以 Agent 的 `mcp/catalog-changed` 快照事件推断健康。

当前 `GET /v1/mcp/servers` 已能提供 `status`、`last_error`、`tool_count`、`enabled_tool_count`、`last_refresh`、`last_checked`、`observed_at`、`status_revision`、`health_stage`、`failure_kind`、`retryable`、`stderr_summary` 和 `exit_code`；`GET /v1/mcp/status` 返回 Node 级聚合健康状态。MCP Manager 在检查阶段、检查结果变化和单次调用失败时发布 Node 级 `mcp/status-changed` 事件，前端状态栏以事件为主、低频查询作为断线恢复兜底。stdio Client 的有界 stderr/退出码和 call failure 生命周期已接入；独立专用 Node SSE 不是必需项，当前复用全局 Node stream。

### 4.2 P1：从模型可见工具中移除 `linux_exec`，统一建立 Terminal Session

历史上存在 `linux_exec(config_id, command)` 与 `terminal_open(config_id)` 两条远程执行路径，模型需要自行选择；`linux_exec` 每次创建独立会话，不保留 cwd、环境和进程状态，文件传输又直接使用 `config_id`。这会增加工具选择、权限和生命周期的不一致。

当前已从新 Agent 默认工具快照中移除 `linux_exec`，保留底层一次性命令 Provider，并新增基于 Terminal Session 的工具。推荐模型侧统一为：

```text
terminal_config_list
        ↓
terminal_open
        ↓
terminal_command / terminal_input
        ↓
terminal_read
        ↓
terminal_upload / terminal_download
        ↓
terminal_terminate
```

命令和传输工具都要求 `terminal_id`，由已建立的会话绑定目标、shell、cwd、Agent 和权限。`terminal_command` 已返回结构化的 status、exit_code、stdout/stderr、字节数、超时和截断信息；它必须先命中当前 Agent 的 running session，底层可使用独立 exec channel，避免只依赖 PTY 文本解析而丢失退出码和 stderr 语义。

文件传输不应通过 `scp`、`base64` 或 `cat` 等 shell 命令实现。上传下载应以 terminal session 为前置授权和目标上下文，底层继续使用 SFTP/传输通道，以保留二进制完整性、校验、进度、取消和大文件能力。`terminal_open` 不能变成一次审批后无限执行的权限提升，命令、远程写入和文件覆盖仍需遵循各自的风险策略。

迁移分三步：新 Agent 默认不暴露 `linux_exec`；增加基于 `terminal_id` 的统一命令和传输工具；显式包含旧工具名的旧快照仍可使用兼容 handler，兼容历史消息和旧客户端后再彻底移除。终端空闲回收、并发上限、命令取消和传输取消也要纳入同一会话生命周期。

### 4.3 P1：`config_id` 的前缀契约仍不够显式

Linux 执行要求的实际 ID 是 `linux_channel:<channel_id>`，而工具描述仅说“使用 `terminal_config_list` 返回的 config_id”。若模型或用户把裸 channel ID 传入，会得到“channel not found”。

建议：`terminal_config_list` 输出同时提供机器可用的完整 `config_id` 和单独的 `channel_id`；工具 schema 明确 `config_id` 必须原样复制；Linux 示例直接展示 `linux_channel:` 前缀；服务端错误中返回可诊断的“应使用 terminal_config_list 返回值”。Terminal Session 化后，后续工具优先使用 `terminal_id`，不再重复传递裸 channel ID。

### 4.4 P1：Linux 通道的 `approval_mode` 缺少 API 层枚举校验

复制的旧配置含有 `special_rules`。绑定 API 接受了该值，但运行时才报 `unsupported Linux channel approval mode "special_rules"`。这会把配置错误推迟到真实工具调用，降低可诊断性。

建议：在创建/更新/绑定接口统一校验允许值（空值、`auto`、`allow`、`never`、`require_approval`、`always`、`deny` 等当前 provider 支持值），对旧值提供迁移或明确提示，并让 UI 使用同一枚举来源。

### 4.5 P1：Shell 上下文与终端类型的前端展示需要完善

终端后端已经正确返回 `terminal_id`、`config_id`、`target_kind`、`target_id`、`shell`、`cwd` 和 `status`。当前类型区分为：本机 PowerShell=`local/powershell`，本机 WSL=`local/wsl`，本机 CMD=`local/cmd`，远程 Linux=`linux_channel/bash`。因此 API 和 `terminal_list` 能够正确区分本机、WSL 与远程 Linux。

当前前端已改为使用 `target_kind`、`target_id`、`shell` 和远程通道摘要生成目标标签；最新浏览器回归实际显示“远程 Linux · bash”，并在终端列表中保留“运行中”状态，不再把远程目标简化为裸 `bash`。由 Agent `terminal_open` 创建的会话带有 `config_id`；由 UI WebSocket 直接打开的会话可能只带 `target_kind`/`target_id`/`shell`，但类型本身仍然正确。

此外，`terminal_open` 的描述应把 shell 与命令语法绑定，避免模型在 WSL bash 中发送 PowerShell `Write-Output`。配置列表和打开结果都应重复返回 shell 与目标，`terminal_input` 明确要求使用当前 shell 语法。

### 4.6 P1：终端终止结果需要更精确地区分“已清理”和“优雅退出”

WSL 终端终止时返回 `exited=true`、`status=terminated`，同时嵌套 `exit.Error=The handle is invalid`、`forced=true`、`graceful=false`。当前模型能说明强制终止，但上层若只看顶层状态，可能把“强制清理且句柄异常”展示成普通成功。

建议：统一终止结果的 `status`/`termination_status`/`cleanup_error` 语义；将句柄异常标为 warning 或 failed-cleanup；UI 不要只根据 `exited` 显示绿色成功。

### 4.7 P2：Shell 输出默认格式可能制造无效上下文噪声

PowerShell 的 `Get-ChildItem` 默认格式化输出会把 5 个目录项扩展成约 7KB。工具的截断机制正常，但模型若只需要文件名，应优先使用 `Select-Object -ExpandProperty Name` 等精简命令。

建议：在 bash/terminal 工具描述和系统提示词中增加“先限定字段、避免默认格式化”的例子；对常见目录列表提供结构化或摘要模式；把 stdout 字节数、截断标记、stderr 分开保留。

### 4.8 P2：异步回灌需要在 UI/审计模型中与用户消息严格区分

异步回调已经能够正确驱动模型继续完成 turn，但 hydrate 中回灌内容表现为一条内部 user 形态的消息。对模型上下文可以工作，对前端和审计则存在被误认为用户新输入的风险。

建议：保留独立的 `async_tool_result` / `tool_result` 事件类型和来源字段；前端按权威事件更新“后台运行/已回灌/已完成”，不要把内部回灌当普通用户气泡；评测中增加“回灌不重复执行、不改变用户意图”的断言。

### 4.9 P2：真实质量评测还需要从手工证据升级为可重复套件

本轮运行结果已具备明确证据，但主要通过 API 手工驱动。仓库已有 `node/internal/eval` 的场景和断言框架，尚未覆盖完整的真实 LLM 运行矩阵、成本、缓存命中、审批等待和回灌延迟。

建议：把本报告的场景登记为 golden cases，记录工具顺序、结构化 status、最终事实、是否重复调用、turn/step 数、耗时、输入输出 token、prompt cache 命中和人工审批次数；每次变更后用同一套件对比基线。

### 4.10：Terminal 工作台首轮实现与验证边界

当前已落地独立的 `TerminalWorkbench`：终端为主区域，Agent 消息复用 `MainChatPanel` 以紧凑侧栏展示；顶部提供明确的“返回消息”入口；底部人工输入栏可切换“发送到 Terminal”和“发送给 Agent”；路由使用 `view=terminal&terminal_id=` 表达可恢复视图，返回消息页只断开前端订阅，不发送终止命令。

本轮还修正了终端客户端对服务端 `terminated`/`closed` 生命周期事件的处理，避免显式终止后进入无休止的自动重连；修正了工作台在列表状态为“运行中”时误禁用人工 Terminal 输入的问题。前端终端类型标签统一使用 `target_kind`、`target_id` 和 `shell`，不再只显示 `bash` 造成远程目标歧义。

本轮最新构建在隔离 Node `127.0.0.1:18769` 的内置浏览器中已建立页面 WebSocket，验证了新终端先选目标、远程 Linux `devuser@… · bash` 连接、输入命令和输出回显、Agent/Terminal 模式切换、Agent 面板折叠、返回消息页后再次恢复同一 `terminal_id`、MCP 状态事件刷新和真实 Agent turn。真实 Agent 还完成了 `terminal_list`、`terminal_upload`、`terminal_download` 的连续调用；上传与下载结果的 SHA-256 相同。Go API 测试继续覆盖 PTY、断线、resume 和终止协议，前端 `TerminalSession` 测试覆盖 `started`、输出、重连、显式关闭及终止事件；窄屏 820px 回归无横向溢出。

## 5. 最终优化方案

### 第一优先级：统一执行上下文并补齐可诊断性

1. 设计 Terminal Session/Execution Capability，先建立会话，再执行命令和文件传输。
2. 从新 Agent 工具快照移除 `linux_exec`，新增基于 `terminal_id` 的结构化命令工具；保留旧工具兼容一个版本。
3. 上传下载使用 terminal session 的目标与权限上下文，但底层保持 SFTP，不通过 PTY 搬运二进制。
4. 给 Linux `approval_mode` 增加接口级枚举校验和旧值迁移。
5. 统一 `config_id` 完整格式，后续操作优先使用 `terminal_id`。
6. 规范终端终止结果，区分优雅退出、强制清理、清理异常。

### 第二优先级：MCP 状态可观测性与模型执行质量

1. 增加底部 MCP 聚合状态图标和展开列表；健康状态只由 MCP Manager 权威状态驱动。
2. 增加 Node 级 `mcp/status-changed` 事件、检查时间和状态版本；轮询仅作为恢复兜底。
3. 完善 MCP 子进程诊断与 Windows `npx.cmd` 解析；将初始化超时拆分为安装、启动、握手、工具发现四类状态。
4. 将 shell 类型作为终端会话的硬上下文，前端显示目标类型和远程主机信息。
5. 在系统提示词的任务执行契约中继续强化“以结构化 status 为准、失败不得伪装成功、异步结果不得重复执行、完成必须有证据”。
6. 为运维任务补充“观察 → 修改 → 验证”的显式步骤约束；涉及远程写入时要求执行后复查。
7. 对长输出默认采用字段投影、分页和摘要，避免工具结果挤占上下文并破坏缓存利用率。

### 第三优先级：建立持续评测与发布门禁

1. 把本报告中的 Linux、Bash、terminal/WSL、异步、失败恢复、办公文件和 Draw.io 场景加入 `node/internal/eval` 的 golden suite。
2. 增加失败断言：错误 status、空 stdout、截断 stdout、stderr 告警、强制终止、MCP 初始化失败都不能被最终回答表述为成功。
3. 记录每个场景的 step/turn 数、工具重试数、审批等待、回灌延迟、token 和 prompt cache 命中，形成效果/成本双指标。
4. 在发布前运行 Go 回归、Node API 真实 LLM smoke、UI 清单；OfficeCLI 缺失、浏览器服务缺失等环境能力以 blocked 记录，不作为通过。

## 6. 本轮回归结论

合并后的核心 turn/step、工具结果契约、Linux SSH、交互终端、异步回调和 MCP 运行链路均可工作；真实 LLM 能够依据结构化结果区分成功、失败和异步完成，未观察到把失败工具结果伪报为成功的行为。

本轮已经完成 Terminal Session 化首轮实现、MCP 状态栏首轮实现和工作台真实浏览器回归。当前未宣称所有 Agent 质量提升完成：兼容窗口后的旧工具删除、办公文档能力、长任务/失败恢复 golden suite、成本/缓存基线仍应作为后续质量版本推进。

## 7. 自动回归

以下合并后源码测试已通过：

```text
go test ./node/internal/eval ./node/internal/tools ./node/internal/turn ./node/internal/api
go test ./node/... ./client/... ./shared/config/...
npm test --prefix node/webui/frontend -- --run
```

结果：上述 Go 测试全部通过；Web UI Vitest `47 files / 284 tests` 全部通过；生产构建通过。隔离最新构建的浏览器 UI 已通过导航、真实 WebSocket、远程 Linux 输入、模式切换、面板折叠、返回后 resume、MCP 权威事件刷新、真实 Agent turn 和二进制传输 SHA-256 回环检查。
