# Terminal 工作台 UI 独立修改方案

> 状态：方案与首轮实现已落地，真实远程与 Agent 回归通过（2026-08-22）
>
> 日期：2026-08-22
>
> 范围：Node Web UI 的 Agent 终端工作区，不改变本机 `bash_run` 与 Terminal 的工具语义边界。

当前实现已经落地返回导航、工作台布局、Agent/Terminal 输入分流、同源路由恢复、终端目标标签、终端生命周期映射、Agent 面板折叠/待处理请求自动展开和 MCP 状态入口。最新构建已在隔离 Node（`127.0.0.1:18769`）的内置浏览器中完成真实 WebSocket、远程 Linux 输入、返回后 resume、Agent/Terminal 模式切换、真实 Agent turn、MCP 状态事件刷新和二进制上传下载回环验证；旧工具兼容窗口和持续质量矩阵仍按独立版本节奏推进。

## 1. 背景与目标

当前终端入口是 Agent 消息页中的一个替代视图：选择终端后，消息区被完全隐藏，只显示 `TerminalDock` 和终端输出。这个形态适合查看终端，但不适合作为“人、Agent、终端”共同工作的操作台。

本方案将终端页重新定义为 Terminal 工作台，目标是：

1. 用户可以明确返回消息页，且返回后终端会话继续运行；
2. 终端成为工作台的主要区域，Agent 消息以紧凑形式保留在下方或右下角；
3. 用户在同一个输入栏中切换“发送给 Agent”和“发送到 Terminal”；
4. 本机、WSL、远程 Linux 的目标、会话和状态展示统一；
5. 不把本机 `bash_run` 强制改造成 Terminal，也不把终端输出伪装成 Agent 消息；
6. UI 状态全部来自权威的终端生命周期事件或 Agent turn 事件。

## 2. 当前实现审阅

### 2.1 当前视图切换

当前路径仍然是 `/ui/agents/:agentId`，终端工作台由 query 状态 `view=terminal&terminal_id=` 驱动，`ChatView` 负责在消息页和 `TerminalWorkbench` 之间切换：

```text
view 不是 terminal
  → 显示 MainChatPanel

view=terminal
  → 显示 TerminalWorkbench
  → 保留 Agent 紧凑消息区
```

历史形态会导致：

- 终端工作期间用户看不到 Agent 消息；
- Agent 仍可能在后台执行，但用户缺少上下文反馈；
- 消息输入栏也随消息区一起消失；
- 页面状态不能通过 URL 直接表达，刷新或分享终端工作区不友好。

当前首轮实现已通过工作台侧栏、统一输入栏和 query 路由解决前三项；URL 恢复、远程会话恢复和压力相关的协议回归已落地，后续只需继续扩充持续评测矩阵。

### 2.2 返回按钮缺失原因

历史实现中 `ChatView` 已监听 `TerminalDock` 的 `@close`，但组件没有形成返回按钮闭环。当前 `TerminalWorkbench` 和兼容的 `TerminalDock` 都提供明确的“返回消息”按钮，并由 `ChatView` 清理 query 状态。

返回操作必须至少完成以下动作：

1. 清除当前工作台视图状态；
2. 保留 Terminal Session，不发送 terminate；
3. 恢复消息页并保持当前 Agent；
4. 将焦点恢复到 Agent 输入栏；
5. 不创建新的消息、turn 或 transcript 记录。

### 2.3 当前终端输入

`TerminalPanel` 继续通过 xterm 的 `onData` 支持键盘直连；工作台另有 `TerminalWorkbenchComposer` 作为独立人工输入栏，默认不抢占直连键盘。

当前方式适合方向键、Ctrl+C、密码输入和交互式程序，但不适合作为唯一的人机协作输入方式，因为：

- 用户无法明确当前输入是给 Terminal 还是给 Agent；
- 没有统一的发送确认、失败提示和输入历史；
- xterm 区域既是输出区又是输入区，容易误触；
- Agent 消息输入栏与终端输入完全割裂。

### 2.4 当前终端目标展示

当前终端清单和终端页都存在通过 `shell` 或 `target_kind` 推导标题的逻辑。单独显示 `bash` 无法区分“本机 Bash”和“远程 Linux Bash”。工作台必须优先展示目标上下文，例如：

```text
本机 · PowerShell
本机 · WSL
本机 · Bash
远程 Linux · devuser@staging
```

原始 `target_kind`、`target_id`、`config_id` 继续用于逻辑判断，不允许仅依靠展示文本判断终端类型。

## 3. 总体设计结论

采用“底层三层统一、工具语义分开、UI 工作台统一”的方案。

### 3.1 三层统一模型

```text
目标与授权上下文层
  ├─ local / local-wsl / linux-channel / future container
  ├─ target_kind / target_id / config_id
  ├─ Agent 绑定、权限、审批、审计
  └─ connection / terminal lifecycle

终端操作层
  ├─ Terminal Session / PTY / xterm
  ├─ 人工输入到 Terminal
  ├─ terminal_input / terminal_read / terminal_terminate
  └─ terminal output 与连接状态

Agent 对话层
  ├─ Agent transcript 与 turn 状态
  ├─ 人工输入到 Agent
  ├─ submitMessage / HITL / cancel
  └─ Agent 消息和工具摘要
```

三层的边界：

- Terminal 输出永远留在终端画布，不写入 Agent transcript；
- `to terminal` 只走当前 Terminal Session，不创建 Agent turn；
- `to agent` 继续走现有消息 API 和 turn 生命周期，不绕过 `ChatView` 的发送、审批和取消逻辑；
- 工作台只负责组合展示，不重新实现一套 Agent 消息协议。

### 3.2 本机 Bash 与 Terminal 的关系

本机 `bash_run` 继续作为一次性命令工具，Terminal 继续负责持久交互会话。工作台统一展示本机目标和终端状态，但不要求每次 `bash_run` 都先打开 Terminal。

需要连续 cwd、环境或进程状态时，用户或 Agent 使用本机 Terminal；普通一次性本机命令仍保持低延迟的 `bash_run` 路径。

## 4. 工作台信息架构

### 4.1 顶部工作台栏

终端工作台顶部增加固定工具栏：

```text
← 返回消息    终端工作台    [本机 · WSL ▾]  ● 已连接        重连  清空  终止
```

具体内容：

- 左侧：`返回消息` 按钮，带明确文字，不只使用图标；
- 中部：当前目标名称、shell、用户/主机摘要和终端状态；
- 目标切换：切换已存在的 Terminal Session；没有会话时可选择新建目标；
- 右侧：重连、清空输出、终止会话等操作；
- 终止按钮必须明确表示会结束当前会话，不等同于返回消息页；
- 连接状态来自 WebSocket/Terminal Session 事件，不由是否存在 `terminal_id` 推断。

### 4.2 终端会话列表

工作台内部保留终端 tabs 或下拉列表，展示：

- 目标标签：本机、WSL 或远程 Linux；
- shell：PowerShell、Bash、CMD、sh 等；
- 远程用户与主机摘要；
- 运行中、重连中、已退出、错误等状态；
- 当前会话的 `terminal_id` 仅用于调试或详情，不作为主要标题。

左侧 NavRail 仍可作为终端入口和快速切换入口，但进入工作台后，工作台自身必须提供同等的会话切换能力，不能要求用户返回左栏才能切换终端。

### 4.3 主体布局

推荐桌面端采用“终端主区 + Agent 紧凑区 + 统一输入栏”：

```text
┌──────────────────────────────────────────────────────────┐
│ 返回消息  目标/会话/状态                 重连 清空 终止 │
├──────────────────────────────────────────────────────────┤
│                                                          │
│                    Terminal 主输出区                    │
│                                                          │
│                                      ┌───────────────┐   │
│                                      │ Agent 消息    │   │
│                                      │ 紧凑工作流    │   │
│                                      │ 可折叠/展开    │   │
│                                      └───────────────┘   │
├──────────────────────────────────────────────────────────┤
│ [发送到 Terminal | 发送给 Agent]   人工输入栏      发送 │
└──────────────────────────────────────────────────────────┘
```

推荐默认行为：

- Terminal 输出区占据主要空间；
- Agent 消息以右下角紧凑面板展示，默认高度约 180–260px；
- Agent 面板可折叠、展开为右侧栏或恢复完整消息页；
- 面板展开时不销毁 Terminal Session，不清空 xterm 内容；
- 待审批、用户信息请求和错误等需要操作的消息自动展开 Agent 面板；
- Agent 消息较长时只展示最近活动，提供“查看完整消息”进入消息页。

窄屏端改为上下两级视图：

- 默认显示 Terminal；
- Agent 消息作为底部可拖拽抽屉；
- 输入栏始终固定在底部，抽屉打开时不能遮挡输入栏；
- 更窄屏幕可以使用“终端 / Agent”两个可切换标签，但不能同时销毁任一方的状态。

### 4.4 Agent 紧凑消息区

Agent 区域应复用现有 `MainChatPanel` 的消息构建、工具摘要、审批和流式状态逻辑，建议拆出可复用的：

- `AgentTranscriptView`：只负责消息、工具步骤、审批和滚动；
- `AgentComposer`：负责 Agent 输入、附件、思考控制和发送；
- `RuntimeStatusRail`：负责瞬时 turn/step 状态；
- `MainChatPanel`：消息页中的组合容器；
- `TerminalWorkbench`：终端页中的组合容器。

不能为工作台复制一套简化的消息解析器，否则会再次产生流式结束、hydrate、工具结果和审批状态不一致的问题。

紧凑消息区默认：

- Agent 文本正常显示，但限制可视高度；
- 工具调用使用摘要行，默认折叠长输出；
- 思考正文不在紧凑区展开成长内容，只显示思考状态；
- HITL、失败、取消和最终回复保留清晰的状态颜色和操作入口；
- 提供“打开完整消息页”按钮。

## 5. 统一人工输入栏

### 5.1 模式切换

输入栏左侧使用明确的分段控件：

```text
[发送到 Terminal] [发送给 Agent]
```

模式切换只改变投递目标，不改变当前 Terminal Session 或 Agent turn。

默认模式建议：

- 从终端列表进入工作台时默认为“发送到 Terminal”；
- Agent 有待审批或正在等待用户输入时，自动切换为“发送给 Agent”，但允许用户手动切回；
- 记忆上一次模式时必须按 Agent 维度保存，不能跨 Agent 串用。

### 5.2 发送到 Terminal

输入栏采用终端友好的等宽字体，并明确当前目标：

```text
发送到 Terminal · 本机 WSL · bash
```

建议语义：

- Enter：发送当前行并自动追加换行符；
- Shift+Enter：插入换行，不立即发送；
- Ctrl+Enter：发送多行内容；
- 输入为空时不发送；
- 发送结果通过轻量状态显示“已写入 N 字节”或明确错误；
- 发送不创建 Agent 用户消息，不进入 Agent transcript，不触发 LLM；
- 仍使用当前 Terminal Session 的 WebSocket 和 Agent 所有权校验；
- Terminal 退出、重连或错误时禁用发送，并显示原因。

xterm 直接输入建议保留，但需要明确为“终端直连/高级交互”能力：

- 普通命令默认使用人工输入栏，避免误触和目标不明；
- 点击终端画布或启用“键盘直连”后，xterm 接管方向键、Ctrl+C、密码和交互程序输入；
- 两种输入方式共享同一个 Session，不能各自维护独立缓冲区；
- 直连状态要有明显但低干扰的提示，避免用户误以为输入发给 Agent。

### 5.3 发送给 Agent

“发送给 Agent”复用消息页现有发送链路：

- 使用现有 `submitMessage`；
- 使用现有 turn 状态、SSE、hydrate、HITL 和取消逻辑；
- 在 Agent 紧凑消息区即时显示用户消息和流式回复；
- 保留附件、图片、思考设置等 Agent 输入能力；
- 不把当前终端输出自动拼接到消息中；
- 如果用户需要引用终端结果，应通过显式复制、选择或后续工具读取完成。

Agent 正在执行时的处理必须沿用当前消息页契约：

- 不允许新消息时，输入栏明确显示“本轮执行中”，而不是静默丢弃；
- 有待审批或用户信息请求时，输入栏进入对应交互状态；
- 取消仍通过 Agent turn cancel，不得误调用 `terminal_terminate`；
- Terminal 的终止只影响终端会话，不等同于取消 Agent turn。

### 5.4 输入栏状态与事件

输入栏附近只展示与当前模式直接相关的信息：

| 状态 | 来源 | 展示位置 |
|---|---|---|
| Terminal 已连接/重连/退出 | Terminal WebSocket lifecycle | 顶部工作台栏 |
| Terminal 输入已写入/失败 | Terminal input ack/error | 输入栏右侧轻量提示 |
| Agent 思考/生成/工具执行 | turn/step 权威事件 | Agent 区或输入栏右上方 runtime rail |
| Agent 待审批/等待用户 | HITL 队列事件 | Agent 紧凑区并自动展开 |
| Agent 取消中 | turn cancel lifecycle | Agent 输入栏 |

不使用“最近是否收到文本”“xterm 是否有内容”或本地超时猜测状态。

## 6. 路由与返回行为

### 6.1 推荐使用可恢复的工作台视图状态

终端工作台建议使用 Agent 路由查询参数表达当前视图：

```text
/ui/agents/:agentId?view=terminal&terminal_id=<id>
```

默认消息页可以省略 `view`，或显式使用 `view=chat`。

建议规则：

- 从消息页进入终端工作台时使用一次导航变更；
- 在工作台内部切换终端只替换 `terminal_id`，不制造大量浏览历史；
- 点击“返回消息”清除 `view` 与 `terminal_id`，保留 Agent 路由；
- 刷新页面后根据查询参数恢复工作台和选中的 Terminal Session；
- 当前 Session 仍然由服务端 Registry 管理，前端刷新只重新连接，不重新创建；
- 找不到指定终端时显示明确错误和返回消息按钮，不静默切换到另一个终端。

如果暂时不引入查询参数，也必须实现等价的本地状态机；不能继续依靠 `v-if/v-show` 的隐式切换作为唯一状态来源。

### 6.2 返回消息按钮验收语义

点击后必须满足：

```text
Terminal 工作台
  → 断开当前 WebSocket 显示连接，但不关闭服务端 Terminal Session
  → 恢复 MainChatPanel
  → Agent 消息状态保持最新
  → Terminal Session 仍出现在终端列表
```

再次进入时：

```text
终端列表选择同一 terminal_id
  → 建立 resume WebSocket
  → 回放范围内的输出继续显示
  → 若存在 replay_gap，明确提示输出可能不完整
```

## 7. 与三层统一架构的对应关系

### 7.1 目标与授权上下文层

工作台只接受已经由服务端返回的终端元数据：

- `terminal_id`；
- `config_id`；
- `target_kind`；
- `target_id`；
- `shell`；
- `cwd`；
- `status`；
- 创建时间和安全的用户/主机摘要。

前端不得拼接密码、私钥、known_hosts 或其他凭据，也不得通过修改显示字段绕过 Agent 绑定。

### 7.2 终端操作层

工作台的 Terminal 交互全部基于当前 Session：

- xterm 输出使用 WebSocket；
- 人工输入使用同一 Session 的输入通道；
- 重连使用 session resume；
- 终止使用 terminal terminate；
- 所有操作按 Agent/Terminal 所有权校验；
- UI 不把 Terminal 输出转成 Agent 消息。

### 7.3 Agent 对话层

Agent 紧凑区与消息页共享同一份 entries、turn state、HITL queue 和 runtime event：

- Agent 输入仍由 Agent 消息 API 接收；
- Agent 状态仍由 turn/step 事件驱动；
- 工具摘要和最终回复与消息页保持同一渲染结果；
- 工作台不维护第二份 transcript；
- 终端工作台的打开、切换、返回不触发 hydrate，也不改变上下文缓存。

## 8. 实施拆分建议

### PR-1：工作台容器与返回导航

- 增加 `TerminalWorkbench` 页面容器；
- 增加明确的“返回消息”按钮；
- 修复 `TerminalDock` 的 close 事件闭环，或将其职责迁移到工作台；
- 引入 `view=terminal`、`terminal_id` 路由状态；
- 保证返回/再次进入不终止 Session；
- 统一终端目标标签和状态标签。

### PR-2：消息区抽取与紧凑 Agent 面板

- 从 `MainChatPanel` 抽取 `AgentTranscriptView`；
- 抽取可复用的 `AgentComposer` 和 runtime status rail；
- 工作台增加右下角紧凑 Agent 消息面板；
- 复用现有消息、工具、审批和流式渲染逻辑；
- 增加展开完整消息页入口。

### PR-3：统一人工输入栏

- 增加 `to terminal` / `to agent` 模式切换；
- Terminal 模式接入已有 WebSocket 输入通道；
- Agent 模式复用现有 `onSendMessage`；
- 增加写入确认、失败、禁用和焦点状态；
- 明确 xterm 直连模式与人工输入栏的边界。

### PR-4：响应式布局、无障碍与状态细化

- 桌面端右下角 Agent 面板折叠/展开；
- 窄屏底部抽屉或 Terminal/Agent 标签页；
- 键盘焦点、快捷键、`aria-live` 和状态播报；
- 长消息、长终端输出和 replay gap 的显示策略；
- 视觉回归和性能优化。

## 9. 验证计划

### 9.1 导航与会话保持

- 从消息页进入终端工作台，顶部显示返回消息按钮；
- 点击返回后恢复消息页，Terminal Session 仍运行；
- 再次进入同一终端，使用 resume 而不是创建新会话；
- 切换终端不会误终止旧终端；
- 浏览器刷新后能按 URL 恢复工作台；
- 无效 `terminal_id` 有明确错误和返回入口。

### 9.2 人工输入分流

- `to terminal` 发送普通命令，终端收到完整文本和换行；
- Shift+Enter、Ctrl+Enter、多行输入行为正确；
- `to terminal` 不生成 Agent 用户消息、不触发 LLM；
- `to agent` 生成恰好一条用户消息并进入现有 turn；
- 两种模式连续切换不会串发到错误目标；
- Terminal 退出、重连、取消时输入栏状态准确；
- Agent turn 执行中、待审批、取消中时行为与消息页一致；
- Ctrl+C、方向键、密码输入等交互程序仍可通过终端直连模式工作。

### 9.3 消息与状态一致性

- Agent 紧凑区与完整消息页显示同一条最终回复；
- 流式回复结束后不会消失；
- hydrate 或 SSE 重连后不会重复或覆盖最新 Agent 消息；
- 工具步骤、后台任务、审批和子 Agent 状态显示来源明确；
- Terminal 输出不会出现在 Agent transcript；
- runtime 状态结束后立即清理，不留下过期提示。

### 9.4 终端目标与生命周期

- 本机 PowerShell、Bash、CMD、WSL 标签正确；
- 远程 Linux 显示远程目标、用户和主机摘要；
- `terminal_list` 返回的 `target_kind`、`target_id`、`shell` 与 UI 展示一致；
- 连接中、已连接、重连中、已退出、错误状态均由权威事件驱动；
- 终止按钮只终止 Terminal Session，不取消 Agent turn；
- 返回消息页只断开前端订阅，不关闭服务端会话。

### 9.5 响应式与无障碍

- 桌面端 Terminal 输出区保持主要空间；
- Agent 面板折叠后不遮挡输入栏；
- 窄屏使用底部抽屉或标签切换，不发生内容溢出；
- 键盘可以在 Terminal、Agent 面板和输入栏之间移动；
- 模式切换、连接状态、发送结果具有明确的可访问名称；
- 状态变化通过 `role=status`/`aria-live` 适度播报，不重复朗读高频输出。

### 9.6 回归与性能

- 普通消息页功能不受影响；
- 原有 xterm 连接、重连、回放和终止测试全部通过；
- 长 transcript 不因工作台打开而复制一份完整渲染树；
- 高频 Terminal output 不触发 Agent 消息区重复重建；
- 工作台切换不触发不必要的 hydrate，不改变 prompt cache；
- 终端输出和 Agent 流式消息分别限流、窗口化和销毁。

## 10. 完成定义

满足以下条件后，Terminal 工作台 UI 修改才算完成：

1. 终端页有明确可用的“返回消息”按钮；
2. 返回消息页不会关闭 Terminal Session，再次进入可以恢复输出；
3. Terminal 输出占据主要工作区，Agent 消息以紧凑的右下角/下方区域展示；
4. 人工输入栏可以明确切换 `to terminal` 和 `to agent`；
5. 两种输入路径不会串消息、串 turn 或串终端；
6. Agent 紧凑消息区与完整消息页使用同一套权威状态和渲染逻辑；
7. 本机、WSL、远程 Linux 的目标类型和生命周期状态展示准确；
8. 不改变本机 `bash_run` 与 Terminal 的语义边界，不把终端输出写入模型上下文；
9. 导航、流式、重连、审批、取消、响应式和无障碍验证全部通过。
