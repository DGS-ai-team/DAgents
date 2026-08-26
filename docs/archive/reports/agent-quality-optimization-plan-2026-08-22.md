# Agent 质量优化修改方案与验证计划

版本：v1.1

日期：2026-08-22

关联验证报告：[agent-quality-validation-report-2026-08-22.md](./agent-quality-validation-report-2026-08-22.md)

## 1. 目标与范围

本方案针对本轮真实运行验证发现的问题，重点解决三个方向：

1. 在底部状态栏增加 Node 级 MCP 服务健康状态。
2. 将远程终端、命令执行和文件传输统一到 Terminal Session 生命周期下，逐步移除模型可见的 `linux_exec`。
3. 确保终端列表完整表达本机、WSL 和远程 Linux 的目标类型，并在前端清晰展示。

本方案同时记录目标与落地状态。截至 2026-08-22，输入栏运行态信息栏、Terminal 工作台、Terminal Session 目标标签/生命周期、基于 `terminal_id` 的结构化命令与文件传输、MCP Node 级健康查询/状态栏和 MCP 阶段诊断已落地。最新隔离 Node 的内置浏览器已验证远程 Linux 终端输入、返回后恢复、真实 Agent turn，以及上传/下载二进制文件的 SHA-256 一致性。剩余工作转为持续质量建设：兼容窗口结束后的旧工具移除、办公/长任务/失败恢复等完整真实 LLM 质量矩阵和自动化 golden suite。

## 2. 设计原则

- Node 级状态与 Agent 级状态分离。MCP 健康是 Node 全局状态，不进入对话消息和模型上下文。
- 连接状态、工具暴露状态、模型上下文状态分别表达，不能用一个布尔值混合表示。
- `terminal_open` 建立会话能力，但不自动授予无限命令权限；后续操作仍经过风险策略和审批。
- 交互 PTY、结构化命令执行和二进制文件传输保持不同的数据语义，统一生命周期，不强行统一底层传输方式。
- 新接口优先使用服务端返回的 opaque ID，禁止模型猜测地址、通道 ID 或前缀。
- 旧工具和旧历史需要可迁移、可回放，不能因删除工具导致历史消息无法加载。
- 前端状态必须来自权威 API/SSE 事件，不能通过“最近一次操作”或模糊的本地推断生成。

## 3. 目标架构

```text
Node
├── MCP Manager
│   ├── MCP Server 状态机
│   ├── Node 级健康聚合
│   └── mcp/status-changed 事件
│
└── Terminal Session Registry
    ├── target：local / WSL / linux_channel
    ├── session：shell / cwd / lifecycle / terminal_id
    ├── command execution：结构化退出状态
    └── file transfer：SFTP，复用 session 的目标与权限上下文
```

## 4. MCP 状态栏方案

### 4.1 状态模型

服务级状态建议至少包括：

| 状态 | 含义 | 聚合时是否异常 |
|---|---|---|
| `disabled` | 用户主动禁用 | 否 |
| `offline` / `unknown` | 尚未成功检查或当前不可确认 | 待检查/弱异常 |
| `checking` | 正在启动、刷新或探测 | 检查中 |
| `ready` | 初始化完成、工具目录可用 | 否 |
| `error` | 启动、握手、工具发现或调用失败 | 是 |

当前已有 `status`、`last_error`、`tool_count`、`enabled_tool_count`、`last_refresh`，并已补充：

```json
{
  "last_checked": "...",
  "observed_at": "...",
  "status_revision": 12,
  "health_stage": "checking|initialize|list_tools|catalog|ready",
  "failure_kind": "installation|transport|timeout|authentication|configuration|invalid_catalog|unknown"
}
```

其中：

- `ready` 表示最近一次权威检查成功，不承诺远端服务永久在线。
- `enabled_tool_count=0` 表示服务健康但没有工具暴露，不直接判定为服务故障。
- 错误信息必须脱敏，不展示环境变量值、Authorization、密码或私钥。

### 4.2 聚合状态

聚合对象建议由 Node 侧计算，避免多个前端自行解释：

```json
{
  "status": "healthy|checking|degraded|unconfigured",
  "server_count": 3,
  "enabled_count": 2,
  "healthy_count": 1,
  "checking_count": 0,
  "problem_count": 1,
  "retryable_problem_count": 1,
  "observed_at": "...",
  "revision": 12
}
```

规则：

- 没有启用服务：`unconfigured`，灰色中性图标。
- 所有启用服务为 `ready`：`healthy`，绿色对勾。
- 存在 `checking` 且没有错误：`checking`，蓝色进行中图标。
- 同时存在 `ready` 和 `error/offline`：`degraded`，黄色警告图标。
- 所有启用服务均异常：`unhealthy`，红色警告图标。

### 4.3 前端交互

状态栏折叠时只展示图标、可访问名称和服务数量摘要；点击后展开浮层列表，展示：

- 服务名称和 transport；
- 状态和状态阶段；
- 最近检查时间；
- 工具总数/已启用工具数；
- 脱敏后的最近错误；
- 单服务刷新/重试；
- 跳转 MCP 设置页。

状态图标应放在现有底部状态栏，不放入消息区，不产生 system message，不改变 prompt digest 或工具快照。

### 4.4 权威更新机制

MCP Manager 当前在以下状态转换时发布 Node 级事件：

- 配置加载或保存；
- 服务开始检查；
- 检查开始；
- initialize 成功/失败；
- tools/list 成功/失败；
- MCP 工具调用成功/失败；
- 手动刷新完成。

建议事件格式：

```json
{
  "event_type": "mcp/status-changed",
  "status_revision": 12,
  "server_id": "drawio",
  "status": "ready",
  "health_stage": "list_tools",
  "last_error": "",
  "observed_at": "..."
}
```

事件已通过 Node stream 发布；状态栏已订阅无 Agent 过滤的 `mcp/status-changed` 事件，`GET /v1/mcp/status` 的 30 秒低频查询仅作为断线/事件丢失恢复兜底。`mcp/catalog-changed` 只表达 Agent 工具目录/快照变化，不作为服务健康来源。

### 4.5 MCP Windows 诊断

MCP 启动错误需要区分：

1. npm 安装或 postinstall 失败；
2. Windows 命令解析失败（如 `npx`/`npx.cmd`）；
3. 子进程启动失败；
4. MCP initialize/握手失败；
5. tools/list 失败；
6. 工具调用失败。

错误结果必须保留阶段、退出码、stderr 摘要和 retryable 属性。当前 stdio Client 已以有界缓冲保留 stderr 和进程退出码，Manager 会在 initialize、tools/list、catalog、call 阶段写入诊断，并将 stderr 纳入 installation/timeout/authentication 等分类；状态 API 只返回摘要，不返回环境变量、Header 或密钥。更细的 npm 安装子阶段仍属于后续质量增强。

## 5. 输入栏运行态信息栏

### 5.1 当前问题

当前输入栏下方的状态栏同时承载了多种生命周期不同的信息：模型生成相位、同步执行数量、后台任务数量、待审批数量、上下文压缩、子 Agent 活动和临时输入提示。这些信息会随着 turn/step 状态快速出现和消失，容易造成：

- 输入区下方频繁跳动，视觉上像消息内容的一部分；
- 多个 pill 和状态气泡并列，格式不统一；
- 同一个运行状态可能在消息区、状态栏和输入框提示中重复出现；
- 长任务或多工具任务会挤压模型、思考开关和发送按钮的可用空间。

### 5.2 推荐交互

建议在输入栏右上方增加一个窄的 `composer runtime status rail`，而不是继续扩展输入栏底部状态栏：

- 无边框、无背景块，使用 11–12px 浅灰文字；
- 右对齐、单行展示，超长内容省略；
- 空闲时完全隐藏；
- 运行期间固定在输入栏上方，不改变消息区高度；
- 通过 `role=status` 和 `aria-live=polite` 提供无障碍播报；
- 状态变化只替换同一行文本，不生成消息气泡或新的历史记录。

建议的显示示例：

```text
思考中 · 8s · 工具执行中 2 · 后台任务 1 · 待审批 1
```

当空间不足时按优先级压缩为：

```text
工具执行中 · 2 · 待审批 1
```

完整信息通过 `title`、键盘焦点或点击后的轻量浮层查看。移动端只显示“思考中”“执行中 2”“待审批 1”等短标签，不能让状态栏覆盖输入区。

### 5.3 状态分层

输入栏右上方只展示瞬时运行态：

- `思考中`、`回复生成中`、`准备回复`；
- `工具执行中 N`；
- `后台任务 N`；
- `待审批 N`；
- `正在压缩上下文`；
- `子 Agent 工作中 N`；
- `正在取消`。

输入栏下方保留稳定状态和控制项：

- MCP 聚合健康图标；
- LLM 配置/模型选择；
- 思考开关；
- 连接或配置类稳定提示；
- 必须长期可见的用户操作入口。

“模型生成中”“执行中的数量”等瞬时信息不再放在底部状态栏。MCP 状态属于 Node 级持久状态，应保留在底部状态栏，但不能与 turn 运行态混用。

### 5.4 状态来源与优先级

运行态信息必须由已有权威状态源派生：

| 展示内容 | 权威来源 |
|---|---|
| 模型生成/思考/回复生成 | Turn phase + output channel |
| 工具执行中 | 工具执行事件/当前 step |
| 后台任务 | background job 状态事件 |
| 待审批 | HITL 队列 |
| 上下文压缩 | compression 生命周期事件 |
| 子 Agent 活动 | child-agent/worker 事件 |
| 取消中 | turn cancel 生命周期 |

前端只负责聚合和格式化，不通过“最近一次收到的文本”或本地猜测判断状态。显示优先级建议为：

1. 正在取消；
2. 待审批；
3. 工具执行/后台任务；
4. 思考/生成；
5. 压缩；
6. 子 Agent 数量补充信息。

如果同时存在多个状态，使用单行分隔文本；不再为每个状态创建不同视觉胶囊。状态结束后立即清除对应片段，整行无状态时隐藏。

### 5.5 布局与缓存影响

该信息栏属于纯 UI 投影：

- 不写入 transcript；
- 不发送给 LLM；
- 不改变 system prompt、tool digest 或 prompt cache；
- 不把异步回调、工具结果或运行相位转换成用户消息。

建议使用输入栏容器内的相对定位，将信息栏放在输入 pill 上方并预留安全间距；不要通过动态扩大消息区或压缩输入框高度来腾挪空间。桌面端可展示一行，窄屏端使用短文本和 tooltip/浮层补充详情。

## 6. Terminal Session 统一方案

### 6.1 当前问题

当前模型可见能力存在三套远程入口：

```text
linux_exec(config_id, command)
terminal_open(config_id) → terminal_input/read
linux_file_upload/download(config_id)
```

它们重复表达目标和权限，生命周期不同，容易造成：

- 模型选择错误工具；
- 命令 cwd/环境状态不连续；
- 文件传输绕过终端上下文；
- 审批和取消语义不一致；
- UI 无法用一个会话状态展示全部操作。

### 6.2 目标工具面

新 Agent 默认工具面建议为：

```text
terminal_config_list
terminal_open
terminal_command       # 结构化一次性命令，要求 terminal_id
terminal_input         # 交互输入，要求 terminal_id
terminal_read          # 读取 PTY 输出，要求 terminal_id
terminal_upload        # 要求 terminal_id
terminal_download      # 要求 terminal_id
terminal_list
terminal_terminate
```

`linux_exec` 不再进入新 Agent 的工具快照。底层仍然可以保留一次性命令 Provider，供 `terminal_command` 使用。

### 6.3 Terminal Session 约束

`terminal_open` 成功后返回：

```json
{
  "terminal_id": "...",
  "config_id": "local-wsl|linux_channel:...",
  "target_kind": "local|linux_channel",
  "target_id": "...",
  "shell": "wsl|powershell|bash|cmd",
  "cwd": "...",
  "status": "running"
}
```

后续命令和传输必须使用 `terminal_id`。服务端通过会话解析：

- Agent 所有权；
- 目标类型和目标 ID；
- 当前 shell；
- cwd 和环境策略；
- 会话状态；
- 审批和风险上下文。

不能让后续工具重新接受任意 host、channel ID 或裸路径来绕过会话约束。

### 6.4 命令执行

不建议只用 `terminal_input` + `terminal_read` 模拟所有命令执行，因为 PTY 会合并 stdout/stderr，退出码和超时也不可靠。

已新增 `terminal_command`：

- 必须传 `terminal_id`；
- 返回 `status`、`exit_code`、`stdout`/PTY 输出、stderr（若 provider 支持）、`output_truncated`；
- 明确同步完成、后台运行、失败、超时、取消；
- 已复用已建立 session 的 Agent ownership、target、shell 和 cwd 上下文；底层可以使用独立 exec channel。

交互程序仍然使用 `terminal_input` 和 `terminal_read`。

### 6.5 文件传输

上传下载必须先存在有效 Terminal Session，但不通过 shell 命令传输二进制：

```text
terminal_open
    ↓
terminal_upload/download(terminal_id)
    ↓
底层 SFTP 或等价的二进制传输通道
```

这样可以保留：

- 二进制完整性；
- SHA-256 或大小校验；
- 进度和取消；
- 大文件限制；
- 覆盖策略；
- 传输结果不写入完整消息历史。

`terminal_open` 是目标和授权上下文的前置条件，不代表一次审批后允许无限远程写入。上传、覆盖、下载敏感文件仍需单独执行风险判断。

### 6.6 兼容迁移

当前采用渐进迁移：

1. 新 Agent 默认工具组不暴露 `linux_exec`。
2. 已增加基于 `terminal_id` 的结构化命令和传输接口。
3. 显式包含旧工具名的旧 Agent 快照仍可继续使用兼容 handler；新快照不会自动暴露旧定义。
4. 保留旧工具结果和历史消息的解析能力至少一个版本周期。
5. 新旧工具并存期间禁止同一任务无理由在两套接口之间来回切换。
6. 迁移完成后再删除旧工具定义和专用 provider 入口。

## 7. 终端类型与列表展示方案

### 7.1 当前行为确认

当前 `terminal_list` 和 `GET /v1/agents/{agent_id}/terminals` 已返回：

```text
terminal_id
config_id
target_kind
target_id
shell
cwd
status
created_at
```

现有类型能够表达：

| 终端 | target_kind | shell |
|---|---|---|
| 本机 PowerShell | `local` | `powershell` |
| 本机 WSL | `local` | `wsl` |
| 本机 CMD | `local` | `cmd` |
| 远程 Linux | `linux_channel` | `bash` |

因此后端返回正确；问题在于当前前端主要显示 `shell || target_kind`，远程 Linux 可能只显示为 `bash`，用户看不到它是远程目标。

### 7.2 目标展示

前端建议根据权威字段生成展示标签：

```text
local + powershell       → 本机 · PowerShell
local + wsl              → 本机 · WSL
local + cmd              → 本机 · CMD
linux_channel + bash     → 远程 Linux · user@host
```

原始 `target_kind`、`target_id`、`shell` 仍保留作为逻辑字段，不建议仅为了展示再制造多个互相可能不一致的类型字段。

## 8. 实施顺序与交付拆分

### PR-1：MCP 状态与诊断

- MCP 状态机补充检查时间、阶段和错误分类；
- Node 级状态事件；
- 状态栏聚合图标和展开列表；
- MCP 服务异常、重试和断线恢复测试。

状态：已完成首轮实现。Node 事件、状态栏订阅、阶段/失败类型/retryable、stdio stderr/退出码和 call 失败降级均已落地。

### PR-2：输入栏运行态信息栏

- 从底部状态栏移除瞬时运行态信息；
- 增加输入栏右上方的单行无边框信息栏；
- 统一相位、执行数量、后台任务、审批和压缩的展示格式；
- 增加状态优先级、窄屏和无障碍测试。

状态：已完成首轮实现。瞬时状态已移到输入栏右上方，底部保留稳定控制；窄屏无横向溢出和可访问名称已完成回归。

### PR-3：Terminal Session 合同

- 明确 `terminal_id` 会话合同；
- 新增结构化 `terminal_command`；
- 命令、终端和传输共用目标/权限上下文；
- 增加 session ownership、状态和取消测试。

状态：已完成首轮实现。命令工具使用结构化结果，session 所有权和生命周期由 Node 端解析。

### PR-4：文件传输迁移

- 上传下载改为要求 `terminal_id`；
- 保持 SFTP/二进制传输实现；
- 增加校验、进度、取消和覆盖策略；
- 增加旧 `config_id` 调用的兼容错误提示。

状态：已完成首轮实现。真实远程 Linux 上传/下载已通过，8,192 字节两端 SHA-256 一致；错误路径也已验证为明确失败。

### PR-5：`linux_exec` 弃用与终端展示

- 新 Agent 工具快照移除 `linux_exec`；
- 旧 Agent 兼容别名和弃用提示；
- 前端完整展示本机/WSL/远程 Linux；
- 完善终止状态和异常清理展示。

状态：已完成迁移首轮。新 Agent 的 `terminal` 工具组不暴露 `linux_exec`，旧快照仍保留兼容路径；最终删除旧名称需在兼容窗口后的独立变更中完成。

## 9. 验证计划

### 9.1 自动化单元测试

#### MCP

- 聚合状态：无配置、全部健康、部分异常、全部异常、仅 disabled、checking 转 ready。
- 状态事件：每种权威状态转换只产生一次递增 revision。
- 错误分类：install、startup、initialize、tools/list、call 能被区分。
- 错误脱敏：环境变量、Authorization、密码、私钥不出现在状态事件和 UI 数据中。
- 配置变更：工具 allowlist 变化不错误地清除健康状态；连接配置变化会触发重新检查。

#### Terminal Session

- `terminal_id` 归属校验：跨 Agent 访问必须失败。
- 终端状态：running、exited、closed、forced cleanup 的状态转换。
- `terminal_command` 必须拒绝不存在、已退出或其他 Agent 的 terminal ID。
- 上传下载在没有 terminal ID 时拒绝，在有效 session 下才执行。
- SFTP 二进制上传下载后的大小和 SHA-256 一致。
- 取消和终止不会遗留可用的 session 或后台传输。
- 旧 `linux_exec` 历史结果仍可读取，新工具快照不再包含它。

#### 终端类型

- local/powershell、local/wsl、local/cmd、linux_channel/bash 的字段映射。
- `target_kind`、`target_id`、`shell` 缺失或未知时的降级展示。
- `terminal_list` 与 `/terminals` 返回结构一致。

### 9.2 API 集成测试

#### MCP 状态栏 API

1. 无 MCP 配置时返回 `unconfigured`。
2. 两个服务一好一坏时返回 `degraded`，并保留每个服务的独立错误。
3. 服务刷新后 status revision、observed_at 和 last_checked 单调更新。
4. 子进程退出后能够收到异常事件，不能继续显示为新鲜的健康状态。
5. API 重启或 SSE 断线后，前端可通过列表接口恢复完整状态。

#### Terminal Session API

1. `terminal_open` 返回完整 target、shell、config 和 session 字段。
2. `terminal_list` 能列出多个目标类型，并且字段与打开结果一致。
3. 未打开终端时调用 command/upload/download 返回明确的 `terminal_session_required`。
4. 打开终端后命令和传输使用同一个 session 上下文。
5. 终止后再次使用 terminal ID 必须失败。
6. Agent A 不能读取或操作 Agent B 的终端。

### 9.3 Web UI 测试

#### MCP 状态栏

1. 无配置显示灰色未配置图标。
2. 全部 ready 显示绿色健康图标。
3. 混合状态显示异常图标，并能展开看到具体异常服务。
4. checking 状态显示进行中图标，完成后切换为权威结果。
5. 点击服务行可跳转设置，重试后列表和图标同步更新。
6. 刷新页面、切换 Agent、SSE 断线重连后状态不丢失。
7. 不产生消息气泡、不改变当前对话输入和 turn 状态。

#### 终端列表

1. 显示“本机 · PowerShell”。
2. 显示“本机 · WSL”。
3. 显示“远程 Linux · 用户@主机”。
4. 同时打开多个终端时，切换 tab 不改变目标类型和 session。
5. 终端退出、强制清理、远程断开时显示不同状态。
6. 旧 `linux_exec` 不再出现在新 Agent 工具列表中。

#### 输入栏运行态信息栏

1. 空闲状态下信息栏完全隐藏，不留下空白胶囊或占位文本。
2. 模型思考、回复生成、工具执行、后台任务、待审批、压缩和取消状态均能显示在输入栏右上方。
3. 多种状态同时存在时使用统一的单行浅灰文字格式，不出现多个不同样式的 pill。
4. 状态结束后对应文字立即消失，下一状态不会残留旧文本。
5. 信息栏变化不改变消息区滚动位置、输入框高度和发送按钮位置。
6. 运行状态不写入 transcript，不生成 system message，不影响 turn 或 prompt cache。
7. 窄屏下信息栏不会遮挡输入框、附件按钮、模型选择和发送按钮。
8. 键盘、屏幕阅读器和 tooltip 能获得完整状态描述。
9. MCP 全局状态图标仍保留在底部稳定状态栏，不被瞬时运行态逻辑覆盖。

### 9.4 真实 LLM 场景

#### 运维场景

- 从 `terminal_config_list` 选择配置，不猜测 ID。
- 打开远程 Linux Terminal Session。
- 执行 `uname`、`pwd`、`df -h`、`ls -la` 等只读检查。
- 检查 stdout、stderr、exit_code 和空输出语义。
- 使用同一 session 修改一个测试文件并再次读取验证。
- 模拟路径不存在、命令失败、SSH 断开和终端超时，模型不得伪报成功。

#### 日常办公场景

- 在 Terminal Session 或 fs 工具中生成 CSV/Markdown 数据。
- 回读文件并计算汇总结果。
- 验证 OfficeCLI/LibreOffice 不可用时，模型明确说明能力边界。
- 验证上传、下载后文件内容、大小和校验值一致。

#### MCP 场景

- Draw.io MCP 启动成功后调用 Mermaid 工具。
- MCP postinstall/initialize 失败时，模型和 UI 都显示可诊断错误。
- 一个 MCP 服务异常、另一个健康时，Agent 仍可使用健康服务。
- MCP 工具目录变化后，当前 turn 保持稳定，下一边界应用新工具快照。

### 9.5 性能、成本与缓存验证

每个场景记录：

- turn 数、step 数、工具调用数和重试数；
- terminal_open 建立耗时；
- MCP 初始化和刷新耗时；
- 文件传输吞吐、大小和校验耗时；
- 审批等待时间；
- 输入/输出 token；
- prompt cache hit/miss；
- SSE 状态事件延迟和断线恢复时间。

重点比较移除 `linux_exec` 前后的成本：

- 每次命令是否增加一次 `terminal_open`；
- session 复用能否抵消连接建立成本；
- 是否因工具描述和状态数据增加上下文长度；
- 是否减少错误工具选择和重复调用。

### 9.6 安全验证

- 没有 terminal session 时不能执行远程命令和传输。
- Terminal Session 不能跨 Agent 使用。
- `terminal_open` 的批准不能绕过后续命令和文件写入策略。
- 远程文件覆盖、删除和敏感路径仍需审批或拒绝。
- MCP 状态和终端列表不能泄露密钥、私钥、Authorization 或完整环境变量。
- 终端关闭、Agent 删除、Node 重启后不能遗留可操作的远程资源。

## 10. 发布门禁与回滚

### 必须通过

- Go 全量相关测试通过。
- Web UI 单元和关键交互测试通过。
- MCP 状态聚合与 SSE 断线恢复通过。
- 四种终端类型列表和展示通过。
- 无 session 的命令/传输调用明确失败。
- 有 session 的命令、交互、上传下载和取消全部通过。
- `linux_exec` 旧历史可读，新 Agent 不再暴露。
- 真实 LLM 运维、办公、失败恢复和 MCP 场景通过。
- 输入栏运行态信息栏在所有关键 turn/step 状态下展示正确且不造成布局跳动。

### 可以阻塞发布

- 任意失败状态被模型或 UI 显示为成功。
- 终端或文件传输可以绕过 Agent/session 所有权。
- MCP 服务实际异常但状态栏仍显示全部健康。
- 终端类型把远程 Linux 显示成本机终端。
- 二进制上传下载发生内容损坏。
- 取消或 Node 重启后仍有远程命令/传输存活且不可追踪。

### 回滚策略

- 保留旧 `linux_exec` provider 和历史结果解析，支持配置开关恢复旧工具快照。
- Terminal Session 新接口发生故障时，禁止自动静默降级为无 session 的远程写操作。
- MCP 状态栏故障不应影响 MCP 工具执行，但必须回退到设置页手动查看状态。
- UI 展示回归时保留原始 `target_kind`、`target_id` 和 `shell` 字段，避免数据迁移损失。

## 11. 完成定义

本方案实施完成需同时满足：

1. MCP 状态栏能够准确反映 Node 级服务健康，并可定位具体异常。
2. 新 Agent 不再依赖 `linux_exec` 进行远程命令执行。
3. 命令和文件传输均要求有效 Terminal Session，但文件传输仍使用可靠的二进制通道。
4. `terminal_list` 和 Web UI 能清晰区分本机、WSL、CMD、PowerShell 与远程 Linux。
5. 旧 Agent、历史消息和旧客户端具备明确兼容或迁移路径。
6. 自动化、UI、真实 LLM、性能、缓存和安全验证全部达到发布门禁要求。
7. 瞬时运行态统一展示在输入栏右上方，底部状态栏不再承载会临时出现和消失的执行信息。
