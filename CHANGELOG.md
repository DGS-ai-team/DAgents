# 变更记录

本文档遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 的条目风格；版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

## [0.10.0] - 2026-08-24

**工作组 AgentRef 重构**：工作组成员从“绑定 Node 并创建受限 MemberSpec”迁移为引用 Node 上已有 Agent，并通过 Node 主动连接 Manage 的 WebSocket 建立独立会话。

### 新增

- Node Registrar 将本地已有 Agent 注册到 Manage Registry；Manage 工作组成员可直接选择 `agent_id`。
- 增加 `agent.session.open/close`、`agent.turn.start/cancel`、`agent.turn.event/result` 协议，Manage 只通过已建立的 Node→Manage WS 下发控制。
- 为每个 `workgroup_id + member_id` 建立稳定 `session_id`，隔离上下文、消息队列、工具运行、长期记忆和 LLM 配置；同一 Agent 可并行服务多个工作组会话。
- Node UI 工作组成员弹窗展示已注册 Agent 选择器；过滤 hosting Node 兼容记录，避免选择不可执行的 Node 身份。

### 修复与可靠性

- 重放 `agent.turn.start` 时按 `assign_id` 幂等，避免断线重连重复执行模型 turn，并缓存终态结果供重连回放。
- 修复 AgentRef assign 在 `ready → busy` 后被误报为 session 未就绪的问题。
- 工作组取消通过 WS 下发 `agent.turn.cancel`，并丢弃取消后的迟到结果。
- AgentRef 成员归档改为 `agent.session.close`，open/start/cancel/close 控制帧完成可靠 delivery ACK，避免重连重复回放。
- 为同一 Node 的 resume 与实时 outbox 增加连接级发送串行化，并容忍已被更高游标覆盖的低序迟到 ACK，避免并发投递触发无意义重连。
- 完成事件与历史快照存在微小提交顺序差异时，以流式 assistant 增量和短重试共同确定最终文本。
- 修正工作组页导航栏连接状态复用消息页 SSE 状态的问题，改为展示当前工作组 EventSource 的权威实时事件状态。
- 保留旧 `member.provision/tool.command` 路径，支持迁移期新旧成员并存。

### 测试

- Go 全量 Node/Client/共享配置测试通过。
- Python 全量 189 项、Node Web UI 284 项测试通过；Node 与 Manage Console 生产构建通过。
- 完成隔离端口双进程真实联调：Agent 注册、工作组绑定、会话 ready、真实 WS 消息往返、同一 Agent 两个工作组并发及历史隔离。
- 完成 Node UI 真实检查：工作组成员 ready 状态、Agent 选择器、Node 兼容记录过滤、表单可提交条件和消息展示。

## [0.9.18] - 2026-08-23

**Agent 上下文与终端工作台增强**：提升长对话上下文稳定性、运行时来源可追踪性，以及 MCP/终端操作的可见性和一致性。

### 新增

- 增加结构化消息 `source/provenance`，区分用户输入、运行时上下文、Skill 正文、工具结果、压缩摘要和异步回调。
- 增加 MCP Node 级健康状态、阶段诊断和状态栏入口。
- 增加 Terminal 工作台、终端会话清单、目标类型展示、Agent/终端工作区切换和终端操作状态反馈。
- 增加结构化 `terminal_command` 以及基于 Terminal Session 的终端命令和文件传输能力。

### 优化

- 将运行环境、prompt sidecar 和当天日期迁移到 request-only `ContextInjection`，不再污染 durable history。
- 将 Skill 正文作为独立上下文消息管理，保持 system prompt 稳定并支持版本过滤。
- 改善 MCP 启动、握手、工具发现和调用失败的诊断信息与前端状态映射。
- 统一终端、消息页和底部状态栏的布局、主题和交互行为。

### 兼容性

- 保留旧 `linux_exec` 历史和兼容路径；新 Agent 工具快照优先使用 `terminal_*` 工具。

### 测试

- Go 全量 Node/Client/共享配置测试通过。
- Web UI 284 项测试通过，生产构建通过。
- GitHub PR CI、CodeQL、Python、Tauri、Windows 构建和 Manage 构建全部通过。

## [0.9.17] - 2026-08-21

**Turn/Step 生命周期与远程 Linux 通道稳定性**：让 Agent 运行状态、前端展示和 SSH 主机密钥验证具备明确的权威状态边界。

### 新增

- 建立权威的 turn/step 生命周期投影与 SSE 事件处理，支持刷新恢复、重连、取消和异步回调状态收敛。
- Linux SSH 通道支持严格的 `known_hosts` 与固定主机指纹策略；首次遇到未知主机时展示 SHA256 指纹，确认后才允许固定。
- 增加前端 Agent/workgroup 事件适配器、turn 状态存储、状态看门狗及对应回归测试。

### 优化

- 将生成中、思考中、工具执行等状态统一展示在底部状态栏，移除语义不明确的 Changes 胶囊。
- 改善连续对话、工具调用、取消和刷新恢复时的状态一致性，避免依赖模糊的前端推断。

### 修复

- 修复 SSH 未配置 known_hosts 时被错误归类为握手失败的问题，区分未知主机指纹与指纹不匹配。
- 修复工具回调、取消确认和重连期间前端状态可能停留在生成中的问题。

### 测试

- Go 全量 Node/Client/共享配置测试、Web UI 270 项测试、Manage/Python 185 项测试全部通过。
- 完成连续对话、工具调用、Turn 取消、取消后继续对话、刷新恢复及真实 Linux SSH 通道测试。

## [0.9.16] - 2026-08-20

**工具上下文与 schema 优化**：降低工具描述对模型上下文的占用，并改善 skills 与触发器参数的可发现性。

### 优化

- 精简 shell、terminal、Linux 执行/传输、浏览器状态、图片读取和人工询问等高频工具描述。
- 将启用 skills 后的可用目录放入 system prompt 尾部，并在 Agent snapshot 的 human turn 边界按目录 revision 更新。
- 移除旧的 registry 动态工具描述注入路径，保持工具定义稳定，减少消息级缓存失效。
- 将 trigger condition 改为结构化参数 schema，保留运行时校验作为最终约束。

### 测试

- 增加 skills system prompt、trigger condition schema 和工具描述回归测试。

## [0.9.15] - 2026-08-19

**附件体验与流式工具状态优化**：改善图片/文件附件交互，并修复不同模型返回工具调用时的 Agent 状态展示。

### 新增

- 在输入栏上方以可移除的缩略图卡片展示待发送图片和文件，文件引用不再污染输入文本。
- 在已发送消息后展示图片缩略图，悬停时预览大图，同时避免附件改变消息区和消息气泡布局。

### 修复

- 修复不同 Agent 切换时当前 Agent 的 LLM 配置被其他 Agent 覆盖的问题。
- 修复 Mimo 等模型返回不带 `content` 的工具调用时，思考状态和工具生成状态显示异常的问题。
- 修复工具审批和发送消息后思考状态无法正确恢复的问题。

### 测试

- 补充附件预览、文件引用、流式工具状态和 Agent LLM 配置切换回归测试。

## [0.9.14] - 2026-08-19

**终端与 Linux 通道能力统一**：统一终端与 Linux 通道的工具组配置和 Web UI 展示，并改善工具执行状态的可读性。

### 新增

- 将本机终端与 Linux 通道合并到 `terminal` 工具组，在 Agent 能力与角色设置中集中控制。
- 兼容旧配置中的 `linux` 工具组名称，并在运行时统一归一化为 `terminal`。
- 为混合工具执行清单提供统一工具组图标，避免使用首个工具图标造成误导。

### 修复

- 修复终端与 Linux 工具在独立气泡和合并气泡中的图标、颜色不一致问题。
- 优化运行中雪花动画，减少状态切换时的尺寸和位置跳动。

### CI

- Tauri Linux CI 使用预构建环境，并在镜像发布与检查并行时增加镜像拉取重试。

## [0.9.13] - 2026-08-19

**多厂商 LLM 与模型级思考参数支持**：扩展 LLM Provider 配置，并让思考能力控制适配不同厂商和模型。

### 新增

- 增加 GLM、MiniMax、MiMo LLM Provider 支持及对应的默认地址、模型建议与运行时识别。
- 增加按 Provider/模型提供的思考参数控制，支持不同厂商的开关、思考强度或预算配置。
- 补充 Provider 校验、运行时设置元数据、SSE 推理详情及 Web UI 配置交互。

### 修复

- 修复 MiniMax 固定思考模式不应显示可切换开关的问题。
- 修复不同 Provider 的模型参数与思考控制在前端展示不一致的问题。

### 测试

- 增加多厂商 Provider、模型级思考参数和 Web UI 交互回归测试。

## [0.9.12] - 2026-08-18

**终端执行与工作组协作增强**：补齐 Node 终端能力、MCP 接入和长时任务的可观测性，并继续改善工作组与 Web UI 的稳定性。

### 新增

- 增加独立的终端工具组，支持多终端会话、终端清单、延时读取和取消响应。
- 增加 Linux 通道配置与本地/远程终端接入能力，终端输出统一清理控制序列后再展示和回传模型。
- 增加 MCP Client 配置、工具清单加载及按服务/工具启停控制，并支持按 Agent 绑定启用工具。
- 增加流式响应性能诊断指标，帮助定位 LLM、SSE 和前端渲染链路的卡顿问题。

### 修复

- 修复工作组与 Agent 侧栏刷新时空状态与加载状态切换造成的闪烁。
- 修复终端读取等待期间无法及时响应 turn 取消的问题。
- 修复工具组配置与终端工具展示不一致的问题，并补充相关回归测试。

### 测试

- 增补终端工具组、延时读取、取消响应、终端会话和 Web UI 性能诊断回归测试。

## [0.9.11] - 2026-08-14

**侧边栏与桌面通知稳定性修复**：改善定时刷新体验，并修复远程 WebUI 的通知焦点识别。

### 修复

- 修复侧边栏定时刷新时列表被替换为加载状态、导致内容闪烁或短暂消失的问题。
- 修复多标签页之间相互清除桌面焦点、导致已打开对话仍弹出系统通知的问题。
- 增加 Node 到本机 Shell 的同源焦点转发，支持远程 Node WebUI 正确抑制桌面通知。

### 测试

- 通过 WebUI 213 项、Node API、Go Shell 与 Tauri Shell 焦点回归测试。

## [0.9.10] - 2026-08-14

**工作组历史与实时协作增强**：补齐 Supervisor/成员的完整历史、工具反馈、消息队列恢复和复杂协作场景下的可观测性。

### 新增

- 保留 Supervisor 与成员跨轮次的完整 ActorRun 历史，并支持持久化上下文压缩快照。
- 增加人类消息 FIFO 队列、队列实时快照与“立即发送”恢复路径。
- 为 Supervisor 的成员列表等非任务工具展示安全的 purpose 工具气泡。

### 修复

- 修复工具调用伴随的 assistant 正文被隐藏或错排在工具气泡之后的问题。
- 修复取消、孤儿 turn 和直接成员任务导致消息队列持续排队的问题。
- 修复工作组 ACL/订阅失败时 Node 侧只显示通用发送错误的问题。
- 修复任务卡片、调试面板与消息底部定位在展开和流式执行时的布局问题。

### 测试

- 增补工作组历史、上下文压缩、直接 @成员、消息队列与 Supervisor 工具展示的回归测试。

## [0.9.9] - 2026-08-13

**工作组协作与智能体运行时稳定性更新**：完善异步任务、取消与审批恢复链路，并继续打磨工作组配置体验。

### 修复

- 修复异步工具回调、后台任务和触发器续跑过程中的 turn 恢复与消息队列竞态。
- 修复工具取消、终止和 Windows PowerShell 执行链路，确保取消后能正确闭合 `tool_result`。
- 修复离开对话页后过期的审批状态重复出现。
- 修复工作组成员任务卡片与成员状态展示，降低配置和执行过程中的歧义。

### 变更

- 工作组成员任务详情默认折叠，点击后展示完整任务内容。
- 工作组成员图标改为与侧边栏一致的单色矢量图标。
- 增补异步任务、后台任务和运行时续跑的自动化回归测试。

## [0.9.8] - 2026-08-12

**发布与运行时稳定性修复**：完善 Session 生命周期管理、打包校验与正式发布流程。

### 修复

- Prevent stale hydrate responses from re-enqueuing an already submitted tool approval after navigating to Agent settings.
- 修复 Session runtime 停止与 SQLite 持久化存储关闭之间的竞态，确保 runtime 完成退出后再关闭数据库，降低 `SQLITE_BUSY` 与数据未持久化风险。
- 修复子 Agent 运行时回收与父会话停止过程中的生命周期等待问题，避免自等待死锁。

### 变更

- 增加 required CI、打包冒烟测试和发布 tag/版本一致性校验。
- 完善本地助手、Windows 安装器与 Manage 离线包的发布构建流程。
- 增加正式发布、分支同步和 hotfix 操作规范。

## [0.9.7] - 2026-08-11

**工作组实时协作与 Node Web UI 体验优化**：补齐实时消息可靠性，并统一浅色界面的品牌视觉。

### 新增

- 工作组与普通 Agent 对话支持在上滑后显示“直达底部消息”按钮。
- 工作组 Timeline/outbox 原子写入、断线恢复与多 Node 实时广播隔离。

### 变更

- Node Web UI 浅色主题改为左上角淡蓝色渐变，移除侧边栏绿色渐变。

- **完整 Markdown 消息渲染**：Node 与 Manage 工作组消息改用 GFM 解析，支持任务列表、引用、删除线、图片、代码语言标记与代码高亮，并对 HTML 输出做安全清洗。
- **工作组配置体验**：Manage 成员卡片支持刷新与删除；Node 侧栏新增工作组刷新入口。

- **Manage 工作组多 Node 投递**：resume gap-fill 仅向成员的 `home_node_id` 重放消息，并拒绝 WebSocket header 与 `session.hello` 中不一致的 `node_id`。

- **Manage 工作组运行时 schema**：将 `assign_workgroup_task` 的 OpenAI 工具 schema 移入 Manage 包，避免部署后依赖未打包的 `docs/` 文件。

---

## [0.9.6] - 2026-08-11

**工作组与 Windows 发版稳定性修复**：完善成员 provision 失败反馈、工具集收缩处理和安装器构建流程。

### 修复

- **工作组成员 provision 错误详情**：Manage 保存并展示 `error_code` / `error_message`，Node Dialer 的失败结果可以回写到成员状态。
- **工具集禁用处理**：工具组被收缩时通知当前会话并软拒绝已禁用工具调用。
- **Windows 安装器 CI**：修复 Inno Setup 文本预处理冲突，手动打包与安装器构建恢复成功。

### 变更

- 优化 Windows 安装向导界面，并补充相关工作组与工具集回归测试。

（Git **tag**：`v0.9.6`。）

---

## [0.9.5] - 2026-08-10

**工作组成员配置与发版 CI**：修复成员一直「配置中」；发版构建并行拆分与 Rocky8 browser 缓存。

### 修复

- **工作组成员一直「配置中」（`provisioning`）**：Manage 补 `websockets` 依赖（缺库时 WS 握手 404）；Node Dialer 断线自动重连；provision 失败回写 `member.provision_result(status=error)`。
- **Rocky8 browser CI**：pyenv 缓存卷不可 `rm` 挂载根（`Device or resource busy`）。

### 变更

- **发版 / Manual Package CI**：Windows Go / Tauri Shell / browser 三路并行后再组装；Manage 离线包只等 linux-amd64；Rocky8 预装依赖镜像 + pyenv `actions/cache`。

（Git **tag**：`v0.9.5`。）

---

## [0.9.4] - 2026-08-10

**Linux 命令行首配**：无图形环境可用 `dagents init` 完成与 Web 同源的 onboarding；首配门闸下 Node 就绪探测不再误超时。

### 新增

- **`dagents init` / `setup`**：经 `PATCH /v1/setup/config` 写入身份与 LLM（交互式或 `--yes` 非交互）；`doctor` / help / install 提示对齐。

### 修复

- **首配前 `dagents node` 等待 30s 失败**：就绪改认 `/health`（首配未完成时 `/v1/agent/info` 为 403）；client probe 对 `node_profile_required` 视为已就绪并提示补全首配。

（Git **tag**：`v0.9.4`。）

---

## [0.9.3] - 2026-08-10

**WebUI 稳定性与安装对齐**：对话 SSE/切 Agent 不再卡三点跳动；Linux 安装脚本对齐现网包；内置模板补全 soul/custom；移除已过时的 Tauri Setup 向导。

### 修复

- **WebUI 三点跳动卡住**：切 Agent 时旧 SSE 污染全局 status；`hitl_required` 即结束 awaiting；Hub critical 事件补送；SSE 重连 `after_seq` + hydrate 对账；卡住看门狗；Hub 按 Agent 过滤入队。
- **对话自动滚动失效**：去掉 smooth scroll 与高频 pin 的竞态，stick-to-bottom 恢复。
- **HITL 有选项时误用默认项**：Composer 非空文本优先于选中选项。
- **并行工具空 id 分片**：不再留下僵死「生成中」气泡。
- **Linux 安装脚本**：去掉已移除的 `dagents-cli` 硬依赖；doctor 退出码；update/`--keep-policy`；拷贝 agent-templates。
- **`/clear`**：取消子 Agent 与会话内工具任务，并刷新 Activity。

### 变更

- **内置 Agent 模板**：补 `soul_md` / `custom_md`；创建写入侧车；assemble/install/Inno 打入模板副本。
- **移除 Tauri Setup 向导**（`packaging/bootstrapper`）；保留托盘 Shell（`desktop/tray-tauri`）与 Inno 安装。

（Git **tag**：`v0.9.3`。）

---

## [0.9.2] - 2026-08-07

**Manage Docker 补丁**：镜像打包成员工具权威目录，修复容器启动 `FileNotFoundError`。

### 修复

- **Manage Docker 缺少 `member_tool_catalog.json`**：`packaging/manage/Dockerfile` 仅 `COPY manage/`，容器内 import `member_tools` 即失败；现拷贝 `shared/workgroup/member_tool_catalog.json` 至 `/app/shared/workgroup/`。

（Git **tag**：`v0.9.2`。）

---

## [0.9.1] - 2026-08-07

**Workgroup 协作预览**：正式版前最后一个大预览。以工作组可演示为主叙事；开箱首配与本地助手收口。验收清单见 [`docs/design/v0.9.1-smoke-checklist.md`](docs/design/v0.9.1-smoke-checklist.md)。

### 升级说明（自 0.8.x）

- 运行时目录仍为 `./.runtime`；遗留库视为 **Node 首配已完成**，不会强制重新走身份向导。
- 工作组需启用 Manage（`manage.enabled` + URL）并完成首配后，Workgroup Dialer 才会连接；仅 HTTP 注册 online **不等于**成员工具通道已就绪。
- 成员工具默认仅 **fs**；bash 需在成员配置中显式勾选（无进程级沙箱）。

### 新增

- **工作组（Workgroup）纵向闭环（预览）**：Manage Leader turn + Node Worker；成员 provision / tool.command / journal；Supervisor `assign_workgroup_task`；`@member` 直达；信息型 HITL；取消 turn（含 `tool.cancel`）；Timeline 流式与任务卡片；RunHistory 调试 API / UI。
- **成员工作区工具目录**：权威源为仓库 `shared/workgroup/member_tool_catalog.json`（Node `go:embed`；Manage/Console 读同一文件）。HTTP `GET /v1/workgroups/meta/member-tools` 由 Node 本地提供（**不依赖** Manage 连接）。可执行集为 **fs + bash**；**默认白名单仅 fs**（bash 需显式勾选，无额外沙箱）。
- **浏览器伴生 Agent**：创建主 Agent 且 `enabled_groups` 含 `browser` 时自动创建持久伴生（`{agent_id}-browser`）；主 Agent 仅暴露 `browser_run_task` / `browser_task_status` / `browser_task_cancel`，由 sidecar `browser_use.Agent` 闭环执行；Chrome `session_key` 为伴生 id，独立 profile/debug port，同伴生任务排队。列表隐藏伴生，删除主 Agent 级联归档伴生。默认 `browser.max_sessions` 提升为 8。sidecar 对 Agent 追加 DAgents `extend_system_message`（中文/安全/回传约定）；`browser_task_status` 扁平回灌 `summary` / `success` / `urls` / `screenshot_paths` 等字段。列表时为存量 Agent 补齐伴生；mock LLM 返回中文说明；Web UI 工具摘要展示任务目标与结论。`browser_run_task` 默认 wait=true 同步等待完成并返回 summary；派发前校验伴生记录存在。任务过程归档至 `tasks/<id>.md|json`，伴生提示词注入最近 3 次引用；主对话助手回复下展示可折叠「浏览器引用」。
- **首次进入 Web UI 的 Node 身份首配页**：开箱未完成时全屏三步收集主题、「怎么称呼你 / Node 名称」与一条 LLM 配置，写入 `user.preferred_name` / `agent.name` / `llm.profiles` 与 `onboarding.node_profile_completed`；未完成前拦截本机业务 API（仅放行 bootstrap / setup / LLM 探测 / `/ui` 静态资源与探活），且不启动 Manage Registrar / Workgroup Dialer（升级遗留库视为已完成）。

### 修复

- **首配未完成时托盘打开强制进首配页**：Tauri 检测到 `node_profile_completed=false` 时一律导航/刷新到 `/ui/`（不再同 URL 仅聚焦）；Go 托盘深链同样回落控制台首页。Web UI 在窗口重新可见时会再次检查 bootstrap，避免启动竞态误判为已完成。
- **工作组成员 generation 前进时可重新 provision**。
- **成员工具全部 60s 超时（非 HITL）**：Manage 只等 `tool.result`；Home Node Dialer 未连、binding 仅内存丢失、或失败回 `session.error` 会导致空等满默认 60s。现持久化 WorkerBinding/journal，失败与孤儿 `running` 一律回 `tool.result`；超时报错标明 dialer 未连接。
- **工作组思考提示**：去掉「Supervisor」前缀，进度条与 live assistant 不再双份渲染。

### 变更

- **设置页文案与结构打磨**：技能补无智能体空态并去掉英文 skill；导航「安全」改为「输出防护」并补齐说明；定时任务目标模式中文化并链到能力页轮询；上下文调试标签中文化；关于页帮助去重、「Release notes」改为「更新说明」。
- **能力页布局整理**：对齐其它设置页（去掉重复 intro、按「共享服务 / 运行配额 / 子 Agent 配额」分段、条件区块间距与 placeholder）；字段网格随视口 `auto-fill` 自适应列数。运行中的子 Agent 从能力页移除，改在侧栏「活动」查看/取消。
- **能力页：Web UI 固定挂载**：移除「Web 界面」开关；`/ui/` 始终挂载，setup PATCH 忽略 `ui_enabled=false`。
- **工具组随进程能力显隐**：未启用浏览器服务 / 企业微信推送时，Agent 创建与设置页不再展示对应工具组；`GET /v1/setup/config` 增加 `available_tool_groups`。
- **工作组成员按 Node 工具组配置**：新建/编辑成员时从本 Node `GET /v1/workgroups/meta/member-tools` 拉取工具组清单（fs/bash），勾选组后展开为 `allow_tool_names`。
- **成员 system prompt**：对齐静态规则 → 运行环境 → 工作区 → Soul → 用户信息 → Custom；环境取自 Home Node Registry；强调多成员路径可能不一致。
- **工作组对话 chrome**：顶栏对齐智能体对话；成员弹窗与侧栏交互打磨。
- **收窄 browser Manager / sidecar HTTP op**：Go `BrowserManager` 与 `dagents-browser` 仅保留 session 生命周期与任务级 `run_task` / `task_status` / `task_cancel`；删除细粒度 navigate/click 等 Manager API、`manager_extended`、sidecar `action_runner` 与对应 `op` 分发。文档对齐伴生模型。
- **浏览器引用 UX**：任务截图注册为 media 并在可折叠「浏览器引用」中展示；HITL 审批突出 `browser_run_task` 自然语言目标；工具气泡结果改为「目标 · 短状态」，完整结论留给引用卡片。
- **退役细粒度 `browser_*` LLM 工具**：删除 `browser_ops` 组与 `browser_navigate` / `browser_click` 等 Agent 工具定义与 handler；`browser` 组仅保留 `browser_run_task` / `browser_task_status` / `browser_task_cancel`。sidecar 任务路径与 Manager Start/Stop 保留（后续可再收窄 HTTP op 面）。
- **存量 Agent 策略种子缺项合并**：Node 启动时对 `.runtime/policy/tool.approval.txt` 与 `agents.db` 中全部 `agent_policy` 仅追加 packaging 种子中缺失的工具模式（如 `browser_run_task`），不覆盖用户已改档位；`EnsureAgentPolicy` 读存量行时同样补齐。
- **移除 Node Agent 沙箱**：删除 Docker / process 沙箱运行时（`node/internal/sandbox`、bash docker 路径、模板与 Web UI 沙箱配置）；Agent 一律使用 Node 全局 `fs_root`，工具边界改由工具组与策略控制。`agents` 表 `sandbox_*` 列保留但读写固定为关闭。
- **首配页对齐 Workbench 风格并增加主题步骤**：第一步以三个圆点选择浅色 / 深色 / 跟随系统（写入既有 `dagents_webui_theme`）；整体改用 panel、settings-field、btn 等现有控件样式。
- **首配页不再被 soft 刷新闪进对话页**：窗口 `focus` / `pageshow` 重检 bootstrap 时只允许进入首配，退出仅在用户点「开始使用」完成配置后。
- **侧栏 Agent 列表选中不再置顶**：切换当前 Agent 时保持按 `updated_at` 排序的原有位置，仅用高亮表示选中。
- **Shell 双轨安装**：推荐轨 Tauri（`desktop/tray-tauri`，内嵌 Web UI，需 WebView2）与兼容轨 Go（`desktop/tray`，低版本 Windows）均由 CI 构建；Inno 附加任务二选一写入 `bin\dagents-shell.exe`；未检测到 WebView2 时默认兼容模式。Tauri 轨补齐 Desktop API / SSE 待办 / Toast / 更新编排。
- **远端 Agent Placement 产品路径拆除**（D5）；通信面改走 Workgroup（旧设计稿见 `docs/archive/design/remote-agent-placement.md`）。

### 预览边界（Known limitations）

- 成员侧不提供 browser / skills / triggers / wecom / child_agents。
- 成员 `bash_run` 无进程级沙箱；默认不勾选。
- D05 全量 fixture 执行器未完成（INDEX harness + 部分 golden）。
- Placement / 远端旁观不作为产品能力。

（Git **tag**：`v0.9.1`。）

---

## [0.8.5] - 2026-07-28

**安装向导体验与 HITL 选中修复**：便携 Tauri Setup；HITL 选项回传与 UI 一致。

### 修复

- **HITL 选项回传错位**：ChatView 在模板里写 `hitlSelected.value = v` 因 Vue 自动解包未真正更新选中态，提交时静默落成首项；改为 script 内同步，并按真实选中解析 `selected_options` / `answer`（option `value`）。

### 变更

- **Tauri 安装向导改为便携单文件**：去掉 NSIS「先安装向导再装程序」外层；`dagents-setup-*.exe` 双击即开，编译期嵌入 Inno 并静默落地。
- **Tauri 向导 UI 对齐 WebUI 浅色**：Workbench light tokens、品牌 Logo、Segoe UI 字体栈；窗口图标改用项目品牌图标。

（Git **tag**：`v0.8.5`。）

---

## [0.8.4] - 2026-07-28

**Agent 契约收口与设置体验**：sessions 过渡债清理；沙箱模式选择；LLM 配置可探测模型列表。

### 变更

- **LLM 配置探测模型**：设置 › 连接中新建/编辑档案支持「测试并拉取模型」；Node 代理 `POST /v1/setup/llm/probe-models` 请求兼容 `/models`，成功后下拉选择模型并建议 provider，失败仍可手动填写。
- **沙箱设置 UI**：启用沙箱后选择模式（本机 Docker / 远程预留）并配置对应参数；去掉将 `process` 表述为「沙箱后端」的选项（未启用 = 宿主机 + 工具约束）。
- **沙箱 API**：`backend` 支持 `remote`（需 `remote_endpoint`）；远程运行时尚未实现，启用时返回 `remote_unavailable`。

- **API 错误码对齐**：对外 `session_not_found` 改为 `agent_not_found`，details 使用 `agent_id`。
- **删除 `/v1/sessions*` 410 桩**：不再注册 sessions 路由（404）；错误码 `sessions_moved` 退役。
- **对话持久化表 `agent_runtimes`**：`sessions(session_id, agent_id)` 迁移为 `agent_runtimes(agent_id, node_id)`；Go `store.Record` 字段同步（`AgentID` / `NodeID`）；启动时自动迁旧表。
- **内部/API 错误码**：`session_not_found` → `agent_not_found`（details 用 `agent_id`）。
- **托盘 Desktop UI focus**：`POST /v1/desktop/ui/focus` 请求/响应字段改为 `agent_id`。
- **`stream.Publish` 签名收口**：去掉未使用的 NodeID 第二参，统一 `Publish(agentID, eventType, data)`。
- **Media / hooks 字段**：media artifact 改为 `agent_id`；hook JSON `parent_session_id` 改为 `parent_agent_id`。
- **SSE 信封硬切 `agent_id`**：事件 JSON 去掉顶层 `session_id`；`agent_id` 为对话/Agent 实例 id（不再写 NodeID）。
- **子 Agent 协议硬切 `child_agent_id`**：工具参数、HITL resume、SSE data、Web UI 由 `child_session_id(s)` 改为 `child_agent_id(s)`（`parent_session_id` → `parent_agent_id`）。
- **API 内部命名**：`withAgentAsSession` → `withAgentRuntime`；hydrate/context/ack 等实现改为 `handleAgent*Impl`，path 直接读 `agent_id`。
- **HTTP 硬切 `agent_id`**：`POST /v1/messages` 与 `GET /v1/streams` 过滤只认 `agent_id`；hydrate/context/ack/cancel/clear-context/skills/messages 成功响应去掉顶层 `session_id`。
- **Skills UI 去掉 `enabled` 迁移**：设置页/模板展开不再把旧 `defaults.skills.enabled=false` 改写成工具组；能力只看工具组 `skills`。
- **child-agents HTTP 硬切 `*_agent_id`**：路径改为 `{child_agent_id}`；响应去掉 `parent_session_id` / `child_session_id` 双写（仅 `parent_agent_id` / `child_agent_id`）。
- **Go Client 消息/SSE 发 `agent_id`**：`POST /v1/messages` 与 `GET /v1/streams` 过滤参数改用 `agent_id`；child-agents 列表项增加 `child_agent_id`。
- **Manage Console**：删除未使用的 `sortSessions` 工具函数。
- **文档对齐 sessions 下线**：handbook/契约外的设计与运维文档改为 `/v1/agents`（manage 通信、child-agents、media、smoke 清单、skills context 等）；历史 Shell 设计文保留路径并加移除注记。
- **Go Client Agent 命名**：`GetSessionContext` / `DeleteSession` / `SessionHydrate` 等改为 `GetAgentContext` / `DeleteAgent` / `AgentHydrate` 等；JSON 字段名不变。
- **删除 sessions CRUD 死代码**：去掉未挂载的 `handleCreateSession` / `handleListSessions` / `handleDeleteSession` 及相关响应类型。
- **Go Client / 托盘 Session 别名删除**：移除 `CreateSession` / `ListSessions` / `SessionSummary` / `SyncFromSessions`；统一 `EnsureAgent` / `ListAgents` / `AgentSummary` / `SyncFromAgents`。
- **A2A HITL legacy 删除**：caller 侧仅接受含 `items[]` 的现代 `hitl_required` 载荷，不再转换旧 `approval_args` / `user_information_args`。
- **Skills 能力开关**：运行时不再读 `defaults.skills.enabled`；仅由 Node 总闸 + 工具组 `skills` 决定；写入侧也不再写 `enabled:false`。
- **HITL 旧事件收口**：本机 SSE / WebUI / 托盘 / inbox 等待仅认 `hitl_required`；A2A requires_input 出站强制 `event_type=hitl_required`。
- **Go Client Agent API**：新增 `ListAgents` / `AgentSummary`。
- **Skills payload**：新建/更新 Agent 时 `defaults.skills` 仅写 `visible` 白名单。
- **工具 Registry 装配统一**：默认表与 per-agent Registry 共用 `attachNodeRuntimeDeps`（后台任务回灌、media、browser、manage、wecom、triggers）；去掉 tool-jobs / hydrate 对 `DefaultTools` 的回退。
- **异步回灌可观测**：`EnqueueAsyncToolResult` / `EnqueueToolResult` 在 Agent 不存在时返回 `agent_not_found` 并打 Warn，不再静默丢弃。
- **HITL SSE 收口**：A2A caller 中继统一发 `hitl_required`；子 Agent `RelayHub` 对 `hitl_required` 打 scope。
- **Go Client 迁 `/v1/agents`**：session 相关路径改为 agents；创建/ensure 走 Agent API。
- **LLM 多模态不再广播**：进程级切换 LLM 档案只更新默认 Registry；已装入 Agent 的多模态在 ensure/reload 时按绑定档案生效。
- **Skills 开关收敛**：Agent 技能能力与工具组 `skills` 对齐；可见白名单仍用 `defaults.skills.visible`；Node `skills.enabled` 仍为进程总闸。
- **setup 热挂载**：配置保存后经 `attachNodeRuntimeDeps` 刷新默认工具表，不再单点只挂 WeCom。

### 修复

- **异步 bash 回灌丢回调**：同步超时/转后台与 collector 收尾竞态时，若进程已结束才置 `autoDegraded`，会漏掉 `notifyDone`；现对完成回灌做幂等，并在降级登记时若已结束则立即回灌。`background_job_cancel` / UI 终止后台任务一律触发异步回灌。
- **后台 bash 终止后气泡状态**：终止已转后台的 `bash_run` 后，工具气泡由「后台执行中」更新为「已终止」（改写 transcript 中的 `status=RUNNING`，并以 `/tool-jobs` 为准判断是否仍在跑）。
- **多工具并行气泡状态**：同批免审批 `tool_call` 后端并行执行；未出结果的工具一律显示「执行中」。并行批次中工具完成后立刻推送 `tool_result` SSE；仅 HITL 待批尚未开跑的标「待执行」。

### 新增

- **Agent 可见 Skills 绑定**：创建 / 修改 Agent 时可勾选可见 skills（`defaults.skills.visible`）；运行时 catalog、`load_skills` 与会话可用列表均按白名单过滤。新增 `GET /v1/skills/catalog` 供设置页列举 Node 目录。
- **Tauri 托盘 Shell（预览）**：`desktop/tray-tauri/` 以 Tauri 2 实现系统托盘；支持双击打开 Web UI、启停/重启 Node、health 轮询与单实例；可与 Go `desktop/tray` 并行验证，打包默认仍为 Go Shell。
- **CI：Windows Tauri Shell 构建**：`scripts/ci/build_dagents_shell_tauri.sh` + `.github/workflows/tauri-shell.yml`；`build-and-release` / `manual-package` 的 Windows 作业产出 `dagents-shell-tauri.exe` 预览产物（不替换安装包内 Go Shell）。
- **Tauri 安装向导（可选）**：`packaging/bootstrapper` 提供现代多步 Setup UI，嵌入并静默调用现有 Inno 安装包；未嵌入 payload 时支持演示模式预览。
- **企业微信消息推送工具**：`wecom_send_markdown`（markdown_v2）与 `wecom_send_file`（自动 upload_media）；在设置 › 能力 配置 Webhook，工具组 `wecom`。

（Git **tag**：`v0.8.4`。）

---

## [0.8.3] - 2026-07-23

**配置收敛与 bash 控制体验**：Agent 级参数离开 Node YAML；工具气泡控制时机收紧；进程日志按日拆分。

### 变更

- **`config.example.yaml`**：去掉已废弃的 `expose_to_peers`；移除已迁入 SQLite / Agent 绑定的项（LLM 连接档案、`max_tool_loops` 等）；仅保留 Node 进程级配置。
- **`max_tool_loops`**：不再从 Node `config.yaml` / setup API 读写；仅由 Agent 新建时写入 `config_snapshot`（缺省 32），运行时从 snapshot 装入。
- **进程日志按日拆分**：`node|shell|browser-YYYY-MM-DD.log` 为完整日志，同日 `*.err.log` 仅错误；Node slog 全量走 stdout、error 额外写 stderr。
- **工具气泡操作按钮**：bash「终止 / 转后台」与执行状态同一行，置于状态左侧，圆角样式，不换行。
- **bash 控制时机**：仅在真正开始执行后展示按钮（参数生成中、审批中不展示）；转后台后气泡保留「终止」；状态栏「执行中 / 后台 / 审批」改为与 Changes 同款 pill。

### 修复

- **Composer LLM 切换**：切换后写入 Agent 绑定并应用到进程 LLM；刷新/ensure 后恢复为该 Agent 绑定的档案（含多模态开关），不再回退显示默认模型。
- **`search_replace` 多行预览**：跨行替换时行号提示由「未知」改为「多行」。
- **后台 bash 终止**：`POST .../tool-calls/{id}/cancel` 可按 `tool_call_id` 终止已转后台的任务。

（Git **tag**：`v0.8.3`。）

---

## [0.8.2] - 2026-07-23

**bash 可控执行与体验修复**：同步 shell 可终止/转后台；托盘与安装器若干体验问题。

### 新增

- **`bash_run` 运行中控制**：同步执行登记 in-flight；`POST /v1/agents/{id}/tool-calls/{id}/cancel|background`；Web UI 工具行提供「终止 / 转后台」，状态栏展示执行中与后台任务数量。

### 变更

- **`bash_run` 超时语义**：仅当模型显式传入 `timeout_seconds` 时超时才自动转后台；省略时走硬上限（默认 600s）终止报错。
- **Windows 安装器**：去掉安装结束后的收尾提示弹窗；说明保留在完成页。

### 修复

- **Web UI**：partial `tool_call` 不再过早封存 assistant，避免同一条流式回复被拆成两个气泡。
- **托盘**：审批后避免因焦点过滤导致待办 Toast 重复弹出。
- **Windows 安装向导**：侧栏中文副标题乱码（改用含 CJK 字形字体生成侧栏图）。

（Git **tag**：`v0.8.2`。）

---

## [0.8.1] - 2026-07-22

**Docker 沙箱与长期记忆**：可选 Docker 隔离执行 `bash_run`；`remember` 工具与结构化长期记忆；Agent 可无模板创建并管理模板。

### 新增

- **Docker 沙箱后端**：`sandbox.backend=docker` 时 Agent 入内存预创建常驻容器（Alpine 3.20），`bash_run` 经 `docker exec`；空闲 15 分钟或卸出内存时回收。镜像见 `packaging/sandbox/`。
- **`remember` 工具**：事实归一化、`add`/`replace` 增量输出与 CAS 乐观锁；结构化长期记忆条目支持**全局 / 独立作用域**。
- **Agent 创建**：支持不选模板直接创建；Web UI 模板管理（查看 / 编辑用户模板）。
- **Prompt / LLM**：侧车 prompt context 仅存 SQLite；LLM 档案修改即时保存。

### 变更

- **Windows 安装向导**：改浅色主题，修复乱码与控件重叠。

### 修复

- **长期记忆加载**：仅在清空对话、首条用户消息、上下文压缩后重新加载 longterm，避免每轮重复注入。

（Git **tag**：`v0.8.1`。）

---

## [0.8.0] - 2026-07-21

**Agent 实例模型落地**：单 Node 多 Agent；Web UI 为唯一人机入口；Policy / 侧车 / LLM 按 Agent 配置。破坏性变更，不保证旧 session 数据迁移。设计见 [`docs/design/agent-instance-model.md`](docs/design/agent-instance-model.md)。

### 新增

- **Agent 实例模型（Phase 0–4）**：`node_id` 替代进程级 `agent_id`；模板创建 Agent；可选 per-agent 沙箱 Runtime；Web UI 以 Agent 为首实体（`/ui/agents/:id`）。
- **Agent API**：`/v1/agents/{id}/…`（消息、ack、clear-context、compress、skills、child-agents、policy、prompt-context、workspace-activity 等）。
- **Policy / 侧车按 Agent 存 SQLite**：`agent_policy`、`agent_prompt_context`；Agents 设置页可配置；全局 `/v1/policy*` → 410 Gone。
- **多 LLM 档案**：配置卡片化、Key 加密存 SQLite；输入栏切换档案；Agent 可绑定 LLM；思考开关迁至状态栏。
- **聚合只读 API**：`GET /v1/ui/bootstrap`；`GET /v1/agents/{id}/workspace-activity`。
- **Activity 侧栏**：Cursor 风格「变更与命令」面板；深色 / 浅色主题切换。
- **Agent 完整设置**：创建弹窗 + 模板预设；per-agent 配置页与 runtime reload。
- **托盘**：`GET /v1/agents` 同步待办；Web UI deep link 对齐 Agent 路由。
- **Hook**：`inject_today_date`（turn.before_step）及设置页启停开关。

### 变更

- **移除终端对话 Client**：删除 Go bubbletea TUI 与 Python Textual CLI；`dagents-client` 仅保留 `probe` / `update` / `version`。
- **打包收敛**：以 Node + 内嵌 Web UI 为主；`dagents` / `dagents.cmd` 默认启动 Node 并打印 Web UI 地址。
- **会话语义**：用户可见面统一为 Agent；`/v1/sessions*` 保留但加 Deprecation 头。
- **Manage 注册**：上报 `node_id`（`agent_id` 兼容同值）。
- **连接设置**：去掉全局 `max_tool_loops`；去掉进程级 LLM active 抢占。
- **Web UI**：消息气泡 / 工具标签 / 思考指示器；Composer LLM 自定义下拉；侧栏紧凑列表；origin 本地/远端标识。
- **Windows 安装器**：向导 UI 与 Web Workbench 深色主题对齐；修复 `OuterNotebook.Color` 编译错误。

### 修复

- 删除最后一个 Agent 后侧栏刷新；历史恢复过滤注入消息与并行工具展示。
- Child Agent HTTP cancel 与沙箱单测 TempDir 清理竞态。
- Manual Package 补上 `dagents-shell` 构建。

（Git **tag**：`v0.8.0`。）

---

## [0.7.5] - 2026-07-10

**Web UI 体验**：Composer 附件入口与设置页布局优化。

### 变更

- **Composer 附件**：图片/文件 icon 按钮移至输入框上方工具栏（与「思考」同排）；SVG 线型图标；图片与附件分路处理（预览 vs 路径插入）。
- **设置页布局**：右侧主区域统一滚动，去掉子 panel 嵌套滚动条；Embedded 分区改为分隔线样式，减少「盒中盒」层级。

（Git **tag**：`v0.7.5`。）

---

## [0.7.4] - 2026-07-09

**Web UI 稳定性 + 设置扩展**：修复 SSE ack 洪泛与运行时 JS 错误；除 Node 监听地址外其余配置迁入设置菜单。

### 新增

- **设置页全量配置（除 Node 地址）**：`GET/PATCH /v1/setup/config` 扩展 `runtime`、`agent`、`child_agents`、`browser`、`tools`、`hooks` 与 `features` 高级项；Web UI 分区面板（通用 / 连接 / 上下文 / 安全）+ `useSetupConfig` 共享 load/save。
- **单测**：`session.test.js`（ack/seq 水位）、`desktopFocus.test.js`（Shell focus 心跳）。

### 修复

- **`clearPartialToolIndex is not defined`**：`transcript.js` 补全从 `toolStream.js` 的 import。
- **`POST /ack` → `ERR_INSUFFICIENT_RESOURCES`**：流式 SSE 不再每条事件 ack；与 Node `notify_seq` 对齐，仅在 `done`（回合完成）与 HITL 事件 ack；hydrate 仍立即 ack `sse_seq_hint`。
- **SSE seq 水位**：合并 `lastAppliedSeq` 与 `transcriptStore.lastSeq`；仅在事件实际处理后推进 seq；子 Agent 跳过渲染的事件仍计入去重水位。
- **Shell UI focus 重复上报**：`desktopFocus.js` 统一管理心跳与 1s 去重；从 `hydrateSession` 与 `sessionId` watch 移除重复 POST。

（Git **tag**：`v0.7.4`。）

---

## [0.7.3] - 2026-07-09

**Web UI 设置化 + 上下文体验**：安装向导不再配参；连接/压缩阈值写入 config.yaml；OpenAI thinking 与上下文占比环。

### 新增

- **设置 › 连接**：`GET/PATCH /v1/setup/config`，Web UI 配置 LLM、Manage、功能开关；`shared/config.SaveFile` 原子写 YAML；保存后 `llm.SyncFromConfig` 热同步 provider/model/mock。
- **设置 › 上下文 › 压缩阈值**：配置 `compression.silent_trigger_tokens` / `blocking_trigger_tokens` 及 idle 自动压缩参数。
- **ContextMeter**：Composer 状态栏右侧上下文 **已用占比** 进度环（相对 blocking/silent 阈值）；恢复 token usage 与缓存命中率展示。
- **OpenAI thinking**：`provider=openai` 支持 `thinking` / `reasoning_effort` 出站与运行时热更新（与 DeepSeek 同形）。
- **ToolSummaryRow**：长参数 `tool_call` 进行中显示 spinner、本地计时与边框呼吸动画（不流式展示参数正文）。

### 变更

- **Windows 安装器**：Inno 向导不再配置 LLM / Manage / 功能开关；首次安装复制 `config.example.yaml`；完成页引导 **设置 › 连接**。
- **Web UI 清理**：移除 `AppHeader`、`SessionsPanel`、`ChildAgentsPanel`、`RuntimeStatusPanel`、`StatusLineBubble` 等孤立组件与死 store/API；统一 `utils/usage.js` 解析 usage。
- **Node 清理**：移除 `EnvOpenAIClient`、journal `normalize` 包装、未使用的 HITL 导出等；history 直接使用 `llm.MessageToJournalPayload`。
- **设置页 overlay**：关闭按钮加 `data-panel-close`，嵌入设置页时隐藏以免误点。

### 修复

- **usage 条截断**：移除 `.chat__input-strip-right` 的 `max-width: 55%` 限制。
- **OpenAI adapter**：tool_calls 对齐时保留 `reasoning_content`。

（Git **tag**：`v0.7.3`。）

---

## [0.7.2] - 2026-07-09

**Shell 待办提醒 + 安装包 CI 修复**。

### 新增

- **F-N10 托盘图标闪烁**：有待办 HITL / 未读时，`icon.ico` ↔ `icon_pending.ico` 每 600ms 交替闪烁，并保留 `●` 角标；待办清零后恢复默认 icon。

### 修复

- **Windows 安装包 CI**：Inno Setup `[Code]` 中 `CurUninstallStepChanged` 参数类型 `TSetupUninstallStep` → `TUninstallStep`，修复 Release 构建在编译安装脚本时失败。

（Git **tag**：`v0.7.2`。）

---

## [0.7.1] - 2026-07-09

**品牌图标统一**：程序与文档视觉资源对齐 `desktop/tray/assets/icon.ico`。

### 变更

- **README**：顶部增加品牌图标（引用 `desktop/tray/assets/icon.ico`）。
- **Web UI**：`favicon.ico` / `favicon.png` 与顶栏 `brand-icon.png` 统一为托盘同源图标；`index.html` 优先使用 `.ico`。
- **Windows 安装包**：Inno Setup 向导图标改为 `desktop/tray/assets/icon.ico`（替换默认 Classic 图标）。

（Git **tag**：`v0.7.1`。）

---

## [0.7.0] - 2026-07-09

**跨端与体验收尾**：TUI hydrate、媒体缩略图/lightbox、Windows 更新迁移、A2A relay hydrate、Shell 运维兜底。Smoke 清单（归档）：[`docs/archive/design/v0.7.0-smoke-checklist.md`](docs/archive/design/v0.7.0-smoke-checklist.md)。

### 新增

- **F-H5 Go TUI hydrate**：切换/启动 session 后 `GET /v1/sessions/{id}/hydrate` 恢复 transcript 与 pending HITL。
- **F-H6 Python TUI hydrate**：Textual TUI 同上，复用 Node hydrate API。
- **F-M6 媒体缩略图**：`GET …/media/{id}?thumbnail=1` 服务端缩放（最长边 ≤480px；JPEG/PNG/GIF；WebP 回退原图）。
- **F-M7 Lightbox 下载**：Web UI 列表/气泡用缩略图 URL，lightbox 全屏原图 + 下载按钮。
- **F-M8 TUI 媒体提示**：Go/Python TUI 在 tool_result / hydrate 中打印 media URL 或 path（不渲染像素）。
- **F-ND2 Windows 下线 Node update**：Windows 上 Node 不再 poll Manage；`GET /v1/agent/update` 返回 `delegate=shell`；Client/TUI 自动改读 Shell Desktop API。
- **F-H4 A2A relay hydrate**：`GET /hydrate` 返回 `pending_a2a_relay`；Web/Go/Python TUI 恢复中继 HITL 队列。
- **F-E5 Shell sessions 轮询兜底**：SSE 断线期间仍每 60s `GET /v1/sessions` 同步待办表。
- **F-I5 shell.log**：Shell 进程内 `log` 追加写入 `.runtime/logs/shell.log`。

（Git **tag**：`v0.7.0`。）

---

## [0.6.2] - 2026-07-08

**桌面体验 + Shell 自更新**：路径粘贴、Shell 检查/应用更新、UI focus 抑制 HITL Toast。Smoke 清单（归档）：[`docs/archive/design/v0.6.2-smoke-checklist.md`](docs/archive/design/v0.6.2-smoke-checklist.md)。

### 新增

- **F-ND1 `GET /v1/agent/upgrade-readiness`**：返回 `ready` / `has_active_turn` / 活跃 session 列表；Shell apply 升级前查询 Node 是否空闲。
- **F-I11 `shared/update`**：Manage check URL、manifest 解析、`download_url` 补全、安装包下载与 sha256 校验；Node `UpdateChecker` 改调共享库。
- **F-U5 Shell UpdateChecker**：Shell 后台 poll Manage（`manage.update` 配置），读安装根 `VERSION` 缓存 `UpdateStatus`；**不依赖 Node 在跑**。
- **F-X8 Shell localhost Desktop API**：默认 `127.0.0.1:18767`（update、clipboard、ui focus）；Web UI 跨端口 CORS。
- **F-U6 Shell apply orchestration**：`POST /v1/desktop/update/apply`；查 `upgrade-readiness` → 下载 → stop Node → 覆盖 `bin/*`/`VERSION` → start Node。
- **F-I12 `dagents.cmd update` → Shell**：Shell 在跑时委托 `dagents-shell.exe update`（localhost API）；API 不可达时回退 `dagents-client update`。
- **F-N9 新版本 Toast / 托盘菜单**：Manage 有新版本时 Toast 通知 + 托盘「更新：新版本 x 可用」入口（打开设置 › 关于）。
- **F-X8 Web UI Update 面板**：设置 › 关于优先读 Shell `GET /v1/desktop/update`；Shell 可用时支持页面内一键升级。
- **F-P2 Shell clipboard API**：`GET /v1/desktop/clipboard/files` 读取 Windows `CF_HDROP` 完整路径。
- **F-P1 / F-P3 / F-X4 路径粘贴**：Composer paste/drop 插入绝对路径；浏览器无路径时调 Shell API；多文件换行分隔。
- **F-E9 / F-X5 / F-H13 UI focus 抑制 Toast**：Web UI hydrate/切换 session 时 `POST /v1/desktop/ui/focus`；Shell 抑制同 session 新 HITL Toast（托盘待办仍更新）。

### 修复

- **图片路径**：`show_image` / `read_image` 支持 FS_ROOT 外绝对路径；`MediaRegistry` 注册绝对路径；UI 无 media 时提示。
- **Hydrate 工具行**：按 `blockId` 合并 tool_call + tool_result，避免历史还原成双行。

（Git **tag**：`v0.6.2`。）

---

## [0.6.1] - 2026-07-08

**产品化 Web UI 1.0** + `show_image` + Session Media API。Smoke 清单（归档）：[`docs/archive/design/v0.6.1-smoke-checklist.md`](docs/archive/design/v0.6.1-smoke-checklist.md)。

### 新增

- **v0.6.1 Web UI 产品化（F-UI1–UI6, F-UI8–UI11）**：`vue-router` 聊天/设置路由；两栏聊天布局与产品化顶栏；工具 **一行摘要 + 展开详情**（`ToolSummaryRow`）；Composer 简化（Enter 发送）；HITL sticky 与会话未读/待审批 badge；**设置** 导航（通用 / 技能 / 定时任务 / 安全 / 关于 / 帮助 / 上下文）。
- **F-UI10 定时任务管理**：设置 › 定时任务 — 列表、新建、编辑、启用/禁用、删除（无手动 fire / history）。
- **F-UI11 子 Agent 取消（Web UI）**：设置 › 通用 › 子 Agent 列表；运行中项可 **取消**（`POST …/child-agents/{id}/cancel`）；`/children` overlay 同步支持。
- **F-UI7 system 消息降级**：压缩、子 Agent 生命周期等 system 消息不再插入主消息流（聊天区不展示）。
- **F-UI9 图片 lightbox**：工具结果与用户消息缩略图点击全屏预览（Esc / 遮罩关闭，多图可切换）。
- **设置 › 通用 › 显示思考过程**：控制对话区是否展示 reasoning / thinking 流（`localStorage` 持久化；`/reasoning on|off` 仍可用）。
- **设置 › 上下文**：完整会话消息浏览（`GET …/context?full_messages=1`）；自 Composer 移除「上下文」按钮。
- **Media 轨道（F-M0–M5, F-H10）**：`show_image` 工具；`MediaRegistry` + `GET …/media/{id}`；工具结果与 hydrate 含 `media[]`；`ImageResultPreview` 内联缩略图；F5 hydrate 回放。
- **品牌与顶栏**：雪花 favicon / 顶栏图标；帮助迁入 **设置 › 帮助**；顶栏「设置」改为齿轮图标。

### 变更

- **聊天滚动**：统一滚到底部 + `followTail`；手动上滚暂停跟随，发消息或回到底部恢复。
- **会话侧栏**：按 `updated_at` 降序；当前会话置顶；删除当前会话后自动切换或新建；切换/新建不再插入 system 切换提示；移除副标题「点击切换，+ 新建」。
- **布局**：移除右侧常驻诊断栏；连接状态保留顶栏指示；帮助页与设置页取消 640px 宽度限制。
- **Node**：tool 消息持久化 `Name`；hydrate 回溯 `tool_name`；列表 API 补活跃 session 的 `updated_at` / `first_user_message`。
- **主路径文案中文化（F-UI12）**：流式状态（准备回复 / 思考中 / 压缩上下文）、压缩活动记录等改为中文。
- **流式状态展示（方案 A）**：有正文流式输出后不再单独显示「准备回复」；状态行完成后立即移除；「准备回复」「正在生成」与思考过程流式均为纯三点动画（无文字标签），完成后只保留正文。
- **CSS 拆分（F-UI13）**：`styles/tokens.css`、`styles/layout.css`；`workbench.css` 保留组件样式并通过 `@import` 引用。

### 修复

- **Web UI**：UserInfo 选项提交、`user_message_deferred` 等旁路 SSE 可见；删除会话后仍停留原 session 的问题。
- **工具展示**：`toolUserLabel` 与 tool 占位符；禁止空白 `tool()` 摘要。
- **Hydrate 工具行**：`buildStream` 按 `blockId` 合并 tool_call + tool_result，避免历史还原成「进行中 + 已完成」两行。

（Git **tag**：`v0.6.1`。）

---

## [0.6.0] - 2026-07-08

**Windows Desktop Shell 闭环**：自启 Shell 监护 Node、HITL Toast、Hydrate、IM cursor 未读、idle 卸内存、Windows 安装包含 Shell 登录自启。Smoke 清单（归档）：[`docs/archive/design/v0.6.0-smoke-checklist.md`](docs/archive/design/v0.6.0-smoke-checklist.md)。

### 新增

- **v0.6.0 安装发布（F-I1/I3/I8–I10）**：Windows 包必含 `dagents-shell.exe`；Inno 安装后注册 Shell 登录自启并可选立即启动；`dagents.cmd shell`（start/status/stop）；卸载/升级清理 Run 键并 stop Shell。
- **F-NM1–NM5 idle session 维护（Node）**：`Manager.Release`；idle 扫描内压缩后卸内存；pending HITL 可 evict。
- **F-E13 IM cursor（Node + Web UI + Shell）**：`runtime_state_json` 持久化 `notify_seq`/`ack_seq`；Hub 发布时 bump notify；`POST /v1/sessions/{id}/ack`；hydrate/list 返回 `has_unread`；Web UI SSE/hydrate 后 ack；Shell 待办表改从 `GET /v1/sessions` 同步。
- **v0.6.0 Shell 通知与深链（F-N1–N3/N10, F-U1–U3, F-E13）**：Windows Toast + 点击打开 `?session=<id>`；托盘待办子菜单与 `icon_pending` 态；未读 assistant 待办（IM cursor）；「打开控制台」菜单。
- **v0.6.0 Shell SSE 待办（F-E1–E4/E10–E12）**：`dagents-shell` 常驻订阅 `GET /v1/streams?live=1`；按 session 聚合 HITL/A2A 事件；`GET /v1/sessions` 轮询兜底；可选 `DAGENTS_CLIENT_TOKEN` Bearer 鉴权。
- **v0.6.0 Hydrate API（F-H1/H2/H14/H7–H9/H17）**：`GET /v1/sessions/{id}/hydrate`；Web UI hydrate 管线；evicted session 可恢复。
- **v0.6.0 Shell 基础（F-L8–L10/L12–L13/L15）**：`dagents-shell.exe`；启动 ensure Node、退出 stop Node；Shell/Node 单实例 Mutex；Node crash 自动重启；`scripts/ci/build_dagents_shell.sh` + Release CI。

### 修复

- **Shell Node 启动**：修复 `nodectl.Start` 使用 `CommandContext` 导致 `ctx` cancel 后误杀后台 Node 的问题。

（Git **tag**：`v0.6.0`。）

---

## [0.5.5] - 2026-07-01

**Browser 模式 A 与发布打包**：browser-use 薄服务 `dagents-browser`、Go `browser_*` 工具组、多模态视觉；Agent 身份迁入 `config.yaml`；Windows 安装向导与 PyInstaller CI。

### 新增

- **Browser 模式 A（browser-use 薄服务）**：Python `browser-service/`（`dagents_browser`）驱动本机 Chrome；Go `BrowserManager` + 23 个 `browser_*` 工具（navigate/click/fill/snapshot/extract 等）；设计见 `docs/design/browser-remote-service-mode-a.md`。
- **PyInstaller `dagents-browser`**：CI / 本地打包脚本 `scripts/ci/build_dagents_browser.sh`；Release workflow 与 `dagents-cli` 并列构建；组装进 `bin/dagents-browser[.exe]`。
- **多模态 / Vision**：`multimodal.enabled` + `read_image`；开启后 `browser_*` 自动切视觉模式（`browser_snapshot` 截图注入、`browser_click_coordinate`）。
- **Agent 配置化**：`agent.name` / `description` / `role` / `capabilities` / `metadata` 写入 `config.yaml`，替代工作目录 `agent-card.json`；Manage 注册与 A2A 行为由 `shared/config` 推导。
- **Windows 安装向导**：Inno Setup 三批向导（LLM / Manage / 功能开关）、简体中文 UI；`write-install-config.ps1` 按选项生成 `config.yaml`；启用 Browser 时创建 `.runtime/browser/`。
- **CLI `dagents browser`**：Windows `dagents.cmd` / Linux `dagents` 支持 `browser` / `browser stop` / `--background` 后台启动薄服务。
- **Web UI**：消息气泡支持 tool 参数/结果折叠展示；工作台样式微调。

### 变更

- **A2A 案例与打包**：`cases/a2a-manage-docker` 改用 `agent:` 块；删除 `agent-card.example*.json` 与 `packaging/agent-client/agent-card.*`；`config.example.yaml` 补充 `browser` / `multimodal` / `ui` / `hooks` 注释块。
- **Manage 离线包**：`docker-compose` 补充环境变量示例；assemble 脚本路径对齐。
- **LLM 消息模型**：`ContentPart` 多模态片段；OpenAI provider 透传 image URL / base64。

### 移除

- **`node/internal/manage/agentcard.go`** 及独立 Agent Card 文件路径逻辑；注册 Card 由 config 合成。

（Git **tag**：`v0.5.5`。）

## [0.5.4] - 2026-06-30

**Manage 案例库与制品分发**：案例关联已发布 Skills/Plugins/External Tools、支持附件；Plugins 包 API；Node `/upload` 斜杠命令；Console 列表页与 A2A 修复。

### 新增

- **案例库资源选择器**：Skills / Plugins / External Tools 从已发布目录多选（`ResourcePicker`），保存时校验 catalog 引用。
- **案例附件**：`POST/DELETE /v1/cases/{id}/attachments`；编辑页上传、详情页下载。
- **Manage Plugins 分发**：`POST/GET /v1/plugins/*`；Console **Node 配置 → Plugins** 管理页。
- **Node → Manage 上传**：`POST /v1/manage/upload/{skill|externaltool|plugin}`；TUI / WebUI **`/upload`** 斜杠命令（`manage.enabled` 时）。
- **案例 tool 消息过滤**：导入 JSONL 与 Console 展示时过滤无法解析工具名的 orphan `role=tool` 消息。

### 变更

- **Manage Console 案例库**：卡片列表 + 搜索分页；详情顶栏紧凑化；Registry / Inbox 表格纵向填满视口；移除批量 discovery_group 面板。
- **A2A Task 序列化**：入库/出库前清理非法 UTF-16 surrogate，修复 Admin 任务列表 500。

### 修复

- **页头刷新按钮**：SVG 误用 `btn-icon` 导致文字前空白。

（Git **tag**：`v0.5.4`。）

## [0.5.3] - 2026-06-30

**外置工具目录与 Manage 分发**：`.runtime/externaltools/` 替代 `scripts/`；Manage External Tools API 与 Console 管理页；案例库 `externaltool_ids`；文档收敛至 handbook。

### 新增

- **外置工具目录**：`.runtime/externaltools/`（索引 **`externaltools_menu.md`**）；`externaltools` 包、`bash_run` 描述与 system prompt 同步更新；安装脚本 PATH 加入 `externaltools/`。
- **Manage External Tools 分发**：`POST/GET /v1/externaltools/*`；Console **Node 配置 → External Tools** 管理页。
- **案例库资源**：`resources.externaltool_ids` 与 Skills、Plugins 分列。

### 变更

- **文档清理**：删除 `docs/archive/`、Python 运行时跳转桩与重复的内置工具参考；**handbook** 为唯一正文入口。

（Git **tag**：`v0.5.3`。）

## [0.5.2] - 2026-06-29

**Manage Console 案例库体验**：JSONL 先解析再填元数据；消息列表按 role 分模式展示，tool 消息展示工具名/参数/结果。

### 新增

- **案例新建两步流**：先上传 JSONL 解析，再进入编辑页填写 case 元数据并调整消息列表；`POST /v1/cases/parse-jsonl` 仅解析不落库。
- **CaseMessageList 组件**：默认只读卡片展示；点击「编辑」进入表格模式（前插/改/删）；tool 消息展示工具名、参数（关联 assistant `tool_calls`）、结果。

### 变更

- **Manage Console 表单布局**：LLM / Skills / Releases / Node 管理页统一 `form-block` + `form-grid` 样式。
- **Console 字体**：移除 Google Fonts 外链，改用系统字体栈（代理/离线环境不再报错）。

### 修复

- **VS Code 调试**：补充 Manage 与 Console Vite dev 的 `launch.json` 配置项。

（Git **tag**：`v0.5.2`。）

## [0.5.1] - 2026-06-25

**0.x 预览**：Hook **in-process 插件栈**、**Manage Console**（LLM 配置 / Skills 分发 / PageAgent）、**idle 自动压缩**、Python TUI **上次 session 记忆**等。

### 新增

- **loaded skill 文件保护 Hook**：`builtin.loaded_skill_file_guard`（`tool.before_each`）阻止 `write_file` / `search_replace` / 会改文件的 `bash_run` 修改已加载 skill 目录；`write-skill` 可选 `hooks/protect-loaded-skill/` plugin（见 `node/plugins/`）。
- **无动作自动压缩**：`compression.idle_auto_compress_seconds` / `idle_auto_compress_poll_seconds` / `idle_auto_compress_min_tokens`；后台扫描 idle session，超时且 token 达阈值时 `ForceBlocking` 压缩；压缩后 `runtime_state.idle_auto_compress_applied` 打标跳过重复扫描，用户新对话等入队时清标。
- **压缩摘要 JSONL 脚注**：启用 `raw_message_history` 时，压缩写回前在摘要末尾追加 `history/YYYYMMDD/<session>.jsonl` 指引（`FinalizeCompressionSummary`）。
- **protect-loaded-skill plugin 构建**：源码迁至 `node/plugins/protect-loaded-skill/`（node 模块内 build）；`go test ./node/plugins/...` 与 CI 构建 smoke；packaging `hooks/build.sh` 委托 node 路径。
- **Turn Hook phase 接线**：`turn.error` / `turn.cancel` 在 LLM 失败、流式 cancel、tool 处理 cancel 等路径触发。
- **HITL 大参数诊断**：`scripts/test_python_hitl_large_args.py` 与 `tests/test_cli_hitl_large_args.py`（SSE / HITL 展开 / 入队分层验证）。
- **Open Issue**：[#39](https://github.com/DGS-ai-team/DAgents/issues/39) / [#40](https://github.com/DGS-ai-team/DAgents/issues/40)（同一根因：终端 TUI partial 与 HITL UI 竞态；Web UI 正常）。
- **Python TUI 上次 session 记忆**：退出时写入 `<runtime>/client/last_session.json`；下次 `dagents chat` 未指定 `--session` 时默认复用（按 `api_base` 匹配）；`/switch` 同步更新。
- **Manage LLM 配置注册中心**：`/v1/llm/configs` CRUD + `/resolve` 端点；list/detail `api_key` 掩码（`sk-***last4`），`/resolve` 返回明文 `{model,baseURL,apiKey}`（PageAgent 兼容形）；`is_default` 全局唯一；`allowed_groups` 按 `discovery_group` 命名空间**强制可见性**（非 admin：list 过滤、get/resolve/default-resolve 不可见返回 404）；仅适用于本地/局域网信任部署。多 Node / 外部可按 id 复用；Node 自动消费（热更/同步）延后到 Phase 2，Console 管理页见下。
- **Manage Platform Blob API**：`POST /v1/blobs`（multipart 上传）、`GET/HEAD/DELETE /v1/blobs/{id}`；内容寻址（`blob_id = sha256`），字节落盘 `MANAGE_BLOB_DIR/{sha256}` + 元数据 `{sha256}.json` sidecar（不入 SQLite）；blob_id 严格 64 位十六进制校验防路径穿越；`blob store disabled` 返回 503；供 A2A 文件传输与 Skills 分发共用。
- **Manage Skills 分发（精简版）**：`POST /v1/skills/packages`（multipart，draft；`skill_id`/`version` 限 URL 安全 slug，非法返回 422）+ `POST /v1/skills/packages/{id}/versions/{v}/publish`（单步发布，**幂等**：重复发布不再 bump `catalog_version`）+ `GET /v1/skills/catalog`/`{id}`/`{id}/versions/{v}/download`（仅 published）+ `GET /v1/skills/sync/manifest?since=N`（返回 `{catalog_version, items}` 信封）；多级审批工作流、Node 自动同步（心跳 `skills_catalog_version`→拉取→解压）延后到 Phase 2，Console 管理页见下。
- **Manage Console 集成（Vue SPA）**：新增 **Node 管理** 菜单（含 **LLM 配置** / **Skills** 两个子标签：LLM 配置 CRUD/掩码、Skill 上传/发布/下载），以及全局 **PageAgent 命令栏**——选定一个 LLM 配置后经 `/resolve` 取 `{model,baseURL,apiKey}`、`new PageAgent(...)` 用自然语言操作控制台；`page-agent` 经 npm 依赖 + 动态 import 懒加载分包（不依赖运行期外网 CDN）。
- **Hook in-process 插件栈**：内置 Hook、全局 `hooks.plugins`（`.so` + `Register`）、skill 级 `skills/<name>/hooks/*.so` 统一 `Hook.Run(ctx, *Context, Host)`；`Host` 提供 `SessionStore*` / `LLMComplete` / 只读快照；session 级 `hook_store` 持久化于 SQLite；`llm.before_call` 及多数 lifecycle phase 已接线。
- **write-hook skill**：随包发布 `packaging/runtime/skills/write-hook/`，指导编写 Go Hook plugin（phase、mutation、编译与 config）。
- **`packaging/runtime/plugins/`**：全局 Hook plugin 占位目录说明。

### 变更

- **原始消息 JSONL 目录**：由 `history/<session>_YYYYMMDD.jsonl` 改为 **`history/YYYYMMDD/<session>.jsonl`**（按自然日分子目录）；启用 `raw_message_history` 时 system prompt 工作区说明与 `read_file` 描述补充该路径及 `grep_file`/`read_file` 复盘用法。废弃 command / http / YAML 外部 Hook 与 `packaging/runtime/hooks/` shell 示例；`load_skills` / `unload_skills` / clear-context 同步 skill plugin Registry。
- **压缩侧车 prompt**：已完成/进行中任务三行式说明微调；摘要末尾可保留少量关键指令（≤三行）。
- **protect-loaded-skill 布局**：删除 packaging 下独立 `go.mod`（无法 import `internal/hooks`）；canonical 源码在 `node/plugins/`。
- **Hook Host**：`hooks.host.history_window` 省略或 ≤0 时不截断 Context history（移除默认 50 条上限）。
- **write-skill**：Hook 编写说明拆至 **write-hook** skill。
- **tool.before_each deny**：Hook mutation 支持 `approval_reason`；`ActionDeny` 的 tool 结果文案走 `ToolDenyMessage`（不再固定 `policy_denied`）。
- **前端静态资源不入库**：`node/internal/webui/static/`、`manage/console/static/` 改 CI / `build.sh` 构建；PR 与 Release 工作流补 Manage Console build。
- **全项目唯一版本号**：canonical 仅 `node/internal/version/`；Client/TUI 欢迎区与 `dagents version` / `dagents-client version` 均读 Node `GET /health`（移除 `client/internal/version`、`CLI_VERSION`）。

### 移除

- **`node/internal/hooks/external_*`**（command/http/journal 外部栈）及 `packaging/runtime/hooks/redaction.sh`。
- **Turn 层 dead SSE helper**：`publishUserInformationRequired` / `publishApprovalRequired`（本地 turn 已统一 `hitl_required`；A2A 仍在 session 层发旧事件）。
- **`client/internal/version/`**、Python **`CLI_VERSION`**；已落地的 **`docs/superpowers/plans/`** 实施 plan（设计见 `docs/design/manage-llm-skills-pageagent.md`）。

### 修复

- **Hook Host LLM 配额**：`RunHumanMessageTurn` 开始时重置 `hookHostState.llmCalls`，避免同一 session 后续 user turn 误触 `ErrLLMQuotaExceeded`。
- **Plugin YAML phases**：全局 plugin 注册时用配置 phases 与插件声明 phases **取交集** 约束注册阶段（skill 目录 `.so` 仍用插件自身 phases）。
- **Python TUI HITL（待现场回归）**：审批 UI 在 UI 线程同步弹出（避免 `call_later` 排在大量 `tool_call` partial 之后）；timeout/abort 时 cancel 在途 `_hitl_task`；`search_replace` 流式 partial 不再展示 growing raw JSON。详见 [#39](https://github.com/DGS-ai-team/DAgents/issues/39) / [#40](https://github.com/DGS-ai-team/DAgents/issues/40)。
- **Windows `bash_run` 中文乱码（pipe 模式）**：PowerShell 交互窗口走 `[Console]::OutputEncoding`，stdout 被 pipe 捕获时默认走 `$OutputEncoding`（常为 US-ASCII），中文在 Go 解码前已损坏；`bash_run` 执行前自动注入 `$OutputEncoding = [Console]::OutputEncoding`，与交互式行为对齐。

（Git **tag**：`v0.5.1`。）

## [0.5.0] - 2026-06-21

**0.x 预览**：Turn **旁路侧效应**（Produce / Apply / Continue）落地；**Hooks RunPhase** 框架；异步工具回灌消息对模型更友好；Web UI 流式状态与 deferred 旁路展示对齐 Client。

### 新增

- **旁路侧效应（side-effect）**：`async_tool_result` / `trigger_message` / `a2a_inbox_message` 走 **Produce**（缓冲 + 立即 SSE，不改 history）与 **Apply**（TaskComplete / 步首入库）；`side_effect_continue` 被动续跑 LLM；SSE 事件 `user_message_deferred`、`side_effect_applied`、`side_effects_cleared`、`side_effect_turn_start`。
- **Hooks RunPhase**：`tool.before_each` / `tool.after_each` / `llm.after_call` 等阶段 Hook；内置 duplicate、policy、agent-owned-file、tool_result 压缩；支持外部 Hook 目录加载。
- **异步回灌结构化文案**：user / tool / `tool_callback` 携带 `job_id`、`status`、`call_purpose` 与关键参数摘要；从 history 反查原始 tool call；tool 结果以 `[ASYNC_TOOL_RESULT]` 结构化头 + 正文。
- **Web UI**：`StreamStatusBubble` 展示 LLM / 工具阶段耗时；deferred 旁路与 `tool_callback` 标题解析（含 `call_purpose`）；双栏布局与会话删除交互改进。

### 变更

- **SSE 发布**：turn 层统一为 `sse_publish.go`（`PublishSideEffectCallback` 等）。
- **Turn**：移除 `RunMessageTurn`，单测与 inline 路径对齐；`SetChildAgentManager` / `SetChildSession` 拆分。
- **bash 超时降级**：后台 job 保留 `toolCallID`，便于异步回灌关联原始调用。

### 修复

- **异步 user 消息**：修复 `job_id` 未填入实际值（文案误为字面量「job_id已完成」）。
- **旁路幂等**：`SideEffectAlreadyApplied` 仅匹配 `[ASYNC_TOOL_RESULT]`，避免与 `[TOOL_BACKGROUND] job_id=` 误判。
- **Issue #32**：pending HITL / open batch 期间 Produce 不 inline 改 history 的回归测试与规格文档。

（Git **tag**：`v0.5.0`。）

## [0.4.0] - 2026-06-17

**0.x 预览**：Go Node **内嵌 Web UI**（`/ui/`）随 `dagents-node` 发布；`dagents node` 打印可访问地址；Windows 后台 Node 与安装包打包链路修复；Client/Node 展示与 token 估算小改进。

### 新增

- **Node Web UI**：Vue 3 + Vite 工作台（双栏 Chat / Runtime），`go:embed` 挂载 `GET /ui/`；复用 `/v1` HTTP/SSE，与终端 Client 能力对齐；`ui.enabled` 默认 `true`。
- **Web UI 体验**：输入框上方展示工作中 **子 Agent / 对端 Agent** 数量；`read_file` 结果按扩展名预览（Markdown / HTML / JSON / CSV / 代码 / 纯文本）。
- **启动与发布**：`dagents node` 就绪后打印 `[dagents] Web UI: http://127.0.0.1:<port>/ui/`；CI / Release / `manual-package` 在 `go build` 前构建 Web UI 并跑 Vitest。
- **Client / Node**：文件工具支持 **绝对路径**；TUI **usage 累计**；A2A 工具行展示 **对端 Agent 名**。
- **Token 估算**：抽取共享 DeepSeek estimate，Node / Client 调用方统一。

### 变更

- **打包**：`agent-card.example.json` / `agent-card.example.ops.json` 打入本地助手 bundle 与 Windows 安装包；`webui-url.{bat,sh}` 供启动脚本解析 `listen.port`。

### 修复

- **Windows 后台 Node**：`Start-Process` 脱离控制台，关闭终端后进程不被 kill；`Program Files` 等含空格安装路径下正确启动。
- **Windows 配置路径**：后台启动传相对 `config.yaml`（单字符串 `-config` 引号），避免路径被截断。
- **Web UI 地址显示**：`webui-url.bat` 仅在 `listen:` 段解析数字 `port`；`dagents.cmd` 不再用 `for /f` 截断含冒号的 URL。

（Git **tag**：`v0.4.0`。）

## [0.3.9] - 2026-06-16

**0.x 预览**：A2A **HITL 中继**全链路（Manage 四段式状态机 + caller TUI 展示 + callee inbox 续跑）；双 Client 对齐「`from` 对端标识」与审批后不等 `tool_result`；配套单测与 Docker 验证脚本。

### 新增

- **A2A HITL 中继（caller 侧）**：`agent_invoke` 识别 `awaiting_caller` → `caller_notify` / `WaitCallerHITL` / `caller_resume`；`A2ACallerHITLBridge` 向 caller session 推送 `approval_required` / `user_information_required`（含 `a2a_relay`、`a2a_peer_agent_*`）。
- **A2A HITL 中继（callee 侧）**：`ComplianceExecutor` + `RunInboxTurn` 支持 `requires_input` 与 resume 多步；`encodeRequiresInputPayload` 携带对端 Agent 元数据。
- **Client 展示**：Python / Go TUI 对 A2A 中继审批合成工具块（青色 `from <对端名>`）；审批提交后即终态，不等待本地 `tool_result`。
- **案例脚本**：`cases/a2a-manage-docker/scripts/verify-bash-hitl.sh`（不经 TUI 验证 bash HITL 全链路）。
- **单测**：Node session/manage/a2aclient、Go/Python Client、Manage store 的 A2A 审批 / user_information / 失败路径与边界用例。

### 变更

- **Inbox SSE 订阅**：`RunInboxTurn` 入队前 `Subscribe(afterSeq)`，避免 resume 时回放陈旧 `approval_required`；`done` 先于 HITL 事件时继续等待。
- **Manage Task 状态**：`caller_notified` / `caller_responded` 与 `pending_caller_resume` 拉取后转 `processing`。
- **Agent Card 示例**：`agent-card.example.json`（compliance 被调）与 `agent-card.example.ops.json`（ops 调用方）拆分说明。

### 修复

- **a2aclient**：同一 `requires_input` payload 不重复 `WaitCallerHITL`（`relayedHitlPayload` 去重）。
- **Python TUI**：A2A 中继审批提交后释放 pending 计时动画，避免黄点一直等待 `tool_result`。

（Git **tag**：`v0.3.9`。）

## [0.3.8] - 2026-06-15

**0.x 预览**：TUI **tool_call 流式**去重；Manage **离线 bundle** 与安装 **policy 覆盖**交互；**Agent Card 固定路径**；Manage Admin **移除 session 代理**；工具与通信参考文档。

### 新增

- **Manage 离线 bundle**：`scripts/ci/assemble_manage_bundle.sh` 产出 `dagents-manage-bundle-*.tar.gz`（镜像 + `import-image` / `restart` 脚本 + `docker-compose.offline.yml`）；Release CI 自动附加。
- **安装 policy 交互**：Linux `install.sh`（`--overwrite-policy` / `--keep-policy` + TTY 询问）；Windows Inno Setup 安装时可选覆盖 `_seed/policy`。
- **Agent Card 示例**：`packaging/agent-client/agent-card.example.json`；Node 工作目录固定 `./agent-card.json`（`manage.AgentCardFileName`）。
- **文档**：内置工具参考迁入 handbook 附录；Manage 通信叙述见 handbook/05（历史长文已归档）。

### 变更

- **TUI tool_call 流式**：Full / REPL 共用 `ToolCallStreamState` partial upsert（对齐 call_id 迁移，避免重复行）。
- **Manage 注册配置**：`manage.registration` 移除 `agent_card_path`、`name`、`description`；展示名与 A2A 语义 **仅以 Agent Card 为准**（`name` 空则回退 `agent_id`）。
- **Manage Admin**：移除 `/v1/admin/nodes/.../sessions` 代理与 Console 远程 session UI（Node 会话仍由 Client 直连 Node）。
- **工具 description**：长描述迁入各工具 schema；删除 `descriptions_shared.go`。
- **Cases**：`a2a-manage-docker` / `centos7-feature-tour` README 与配置对齐 v0.3.8（Agent Card 路径、policy `write_file=rule` 信任链说明等）。

### 修复

- **`TestRunMessageTurnMaxToolLoops`**：显式关闭 duplicate hook 配置，避免与「工具轮次上限」断言冲突。

（Git **tag**：`v0.3.8`。）

## [0.3.7] - 2026-06-15

**0.x 预览**：**skills 上下文成本**估算修正；双 TUI **思考内容**展示优化；Manage **离线安装**说明补全。

### 变更

- **skills catalog token 分项**：`skills_catalog_estimated_tokens` 仅反映 `load_skills` 工具 description 中的 catalog 元数据；新增 API 字段 **`skills_catalog_max_body_estimated_tokens`**（单 SKILL 正文上限）；TUI 膨胀告警取二者较大值。
- **catalog 元数据注入路径**：可用 skills 列表经 `Registry.enrichDefinitions` 写入 `load_skills` description，不再拼入 system prompt 元数据段。
- **思考内容展示（双 TUI）**：去掉 `[reasoning]` 前缀，浅灰圆点 + 浅灰正文；`/reasoning on|off` 仍控制是否展示推理流。

### 修复

- **`tools.Registry` × `skills` import**：`skillsCatalog` 字段迁至 `registry_enrich.go` 嵌入体，消除 gopls `missing metadata for import` 诊断。

### 文档

- **Manage 离线安装**：`manage/README.md` 与 `packaging/manage/README.md` 补充镜像导出/导入、`docker compose` 启动与健康检查步骤。

（Git **tag**：`v0.3.7`。）

## [0.3.6] - 2026-06-15

**0.x 预览**：**工具链上下文成本**治理（WS1/3/5/6）：重复调用审批、超长 tool 结果落盘摘要、任务级度量；**移除 `trigger_fire`**。

### 新增

- **`hooks.duplicate_tool_call`**：`tool.before_each` 检测 60s 内同名同参重复调用，在 `rule`+`auto` 审批路径触发 HITL（继续 / 取消 / 强制继续）；详见 [tool-before-hook-duplicate-approval.md](docs/design/tool-before-hook-duplicate-approval.md)。
- **`hooks.tool_result` spill**：`bash_run`、`read_file`、`grep_*`、`search_replace`、`glob_files`、`agent_invoke`、`agent_discover` 等超长结果落盘至 `{fs_root}/tool_outputs/...`，history 仅保留头尾摘要 + `read_file` 提示（默认 `spill_threshold_tokens: 12000`）。
- **`tool_context_metrics`**：turn 结束时 SSE `done` 与日志输出 `tool_loops`、`spill_count`、`status_poll_count`、`history_result_tokens` 等（WS5 基础可观测）。
- **FS 编码缓存**：`read_file` / `grep` 等探测编码后进程内缓存，减少错编码重读轮次。

### 变更

- **bash_run async 引导（WS1）**：超时降级后台 job 的 ACK 文案与自动回灌说明对齐；**不为 `background_job_status` 增加 long-poll**。
- **`bash_run` schema**：移除 `run_in_background` 参数（async 由超时自动降级）。
- **`tools.bash_compress`**：生产路径以 `tool_result` spill 为主；`max_output_chars` 仅保留兼容/测试 clip。

### 移除

- **`trigger_fire` 工具**（**breaking**）：Agent 侧仅保留 trigger CRUD；触发靠 **schedule 调度** 与 **HTTP fire API**（`node/internal/triggers/`）。

### 文档

- 重组 `docs/` 四层索引；`context-compression-and-state` 迁入 `archive/python-agent-runtime/`；新增 [tool-context-cost-analysis.md](docs/design/tool-context-cost-analysis.md) 与 handbook [重大设计变更实录](docs/handbook/附录/重大设计变更实录.md) §2。

（Git **tag**：`v0.3.6`。）

## [0.3.5] - 2026-06-15

**0.x 预览**：system prompt 与 **bash_run** 工具描述优化，减少 LLM 路径/Shell 误用导致的重试。

### 修复

- **System prompt 工作区路径**：保留 `data/`、`memory/` 等子目录说明，但不再暴露 `FS_ROOT` 绝对路径，避免文件工具双重拼接（如 `.runtime/.runtime/...`）。
- **bash_run 平台描述**：Windows 下明确默认 **PowerShell** 语法；非 Windows 明确 **bash**；减少 cmd 命令以 PowerShell 执行导致的失败重试。

（Git **tag**：`v0.3.5`。）

## [0.3.4] - 2026-06-12

**0.x 预览**：上下文压缩与 **Prompt Cache** 对齐；user 消息 **name** 来源标识；**tool_call 流式展示**（双 TUI）；Chat Completions **`user_id=agent_id`**。

### 新增

- **上下文压缩 × Prompt Cache**：侧车 `StreamChat` 与主 turn 前缀对齐（system + tools + messages）；M3 **silent 冷却**抑制重复侧车；`/last_compression` 与压缩 usage 展示；设计实录见 [重大设计变更实录](docs/handbook/附录/重大设计变更实录.md)。
- **User 消息 `name` 字段**：human / trigger / a2a_inbox / child_task / compression / async_tool / compression_sidecar，便于模型区分上下文来源（DeepSeek Chat API）。
- **Tool call 流式展示**：LLM delta 阶段即推送 `partial: true` 的 tool_call SSE；Go / Python TUI 在工具名出现后展示 pending 块，arguments 边流边更新代码预览。
- **LLM `user_id`**：每次 Chat Completions 请求附带 `user_id=agent_id`（DeepSeek 文档推荐，便于服务端观测与限流）。

### 修复

- **压缩死锁**：blocking 压缩与 turn 并发时的锁顺序问题。

（Git **tag**：`v0.3.4`。）

## [0.3.3] - 2026-06-11

**0.x 预览**：DeepSeek **思考开关/强度** 运行时 API；双 TUI 顶栏展示模型、输入条展示 thinking；VS Code 调试配置。

### 新增

- **LLM 运行时 API**：`GET/PATCH /v1/llm/settings`；`GET /v1/agent/info` 嵌套 `llm`（model、thinking、reasoning_effort）；DeepSeek `thinking` + `reasoning_effort`（high/max）热更新。
- **Client `/thinking`**：Go / Python TUI 与 REPL 调用 Node API 开关思考与调整强度（`/reasoning` 仍仅控制推理流**展示**）。
- **TUI 状态展示**：顶栏右侧 **model**；输入框上方 usage **左侧** thinking 状态。
- **开发调试**：`.vscode/launch.json` + `tasks.json`（Go/Python Client 与 Node 联调）。

### 变更

- `config.example.yaml` 补充 `llm.thinking` / `llm.reasoning_effort` 说明；欢迎 Panel 不再重复展示 model/thinking。

（Git **tag**：`v0.3.3`。）

## [0.3.2] - 2026-06-11

### 修复

- **升级/重装不覆盖用户数据**：Linux `install.sh` 与 Windows 安装包升级时始终更新 `bin/` 与启动脚本，`.runtime/` 改为**仅补缺失路径**（`cp -n` / Inno `onlyifdoesntexist`），保留已有 policy、skills、prompt_context、`memory/`、`history/`、`logs/` 等；Windows 升级前会先 `node shutdown`。

（Git **tag**：`v0.3.2`。）

## [0.3.1] - 2026-06-11

**0.x 预览**：**Register Center 移除**，A2A 统一经 **Manage**；Console 迁 **Vue 3**；Go TUI 等待态与工具展示增强。

### 新增

- **Manage Console（Vue 3 + Vite）**：`manage/console/frontend/` 源码 + `build.sh`；Agent 目录、A2A Inbox、Node 抽屉（分组 / session / audit）。
- **Manage discovery_group**：Registry PATCH 分组、Console 批量分配；`agent_discover` / `agent_invoke` **跨组校验**（须共享至少一个分组）。
- **Go TUI 等待反馈**：prefilling / thinking 状态行（动画 + 秒数）、顶栏阶段提示、SSE 断开与长时间无响应告警；plain REPL 等待计时。

### 变更

- **Go TUI 工具展示**对齐 Python TUI：黄/绿/红圆点、pending 动画与耗时、`call_purpose` 标题、参数 `!r` 风格摘要。
- **Manage Docker 镜像**多阶段构建（Node 阶段 `npm run build` Console）。
- 开发栈 / CI / 打包入口：`run_dev_stack.py`、`dagents` 等改为启动 **Manage**（不再启动 Register Center）。

### 移除

- **`register_center/`** 及 `run_register_center.py`、RC 单测与 `dagents_register_center` 打包脚本（**breaking**）。A2A 登记与发现请使用 **Manage Registry**；旧 RC JSON 可经 Manage import 迁移（见 `manage/README.md`）。

### 修复

- **`dagents version`**：展示发版号与子组件版本（`VERSION` 文件、`dagents-cli` / `dagents-node` / `dagents-client version`）。

（Git **tag**：`v0.3.1`。）

## [0.3.0] - 2026-06-11

**0.x 预览**：**Manage A2A（M2）** 与 **双 Node 联调案例** 落地；双 TUI 斜杠命令对齐；**Manage 官方 Docker 镜像**随 Release 发布。

### 新增

- **Manage A2A Task API（M2）**：`POST/GET /v1/a2a/tasks`、inbox long poll、ack/reply；Go Node **inbox poller** + **合规咨询 turn**（`ComplianceExecutor`）。
- **Node A2A 工具**：`agent_invoke`、`agent_discover`；Manage 注册与 Agent Card 上报。
- **案例 `cases/a2a-manage-docker/`**：Manage + 合规/运维双 Node Docker 栈与 TUI 联调说明。
- **`packaging/manage/`**：Manage **Dockerfile**、`docker-compose`、`.env.example`；Release 附带 **`dagents-manage-<version>.tar.gz`** 镜像导出。
- **Python TUI**：`/switch`、`/new`、`/reasoning on|off`；`/help` 中文化。
- **Go TUI**：`/tools expand|collapse` 写入 help。

### 变更

- **双 TUI**：移除斜杠 **`/cancel`**（流式输出中难以输入；**Esc** 取消 turn 为主路径）。
- **Go TUI**：移除 **`o`/`c` 单键** tool 展开/收起，避免无法输入含 `c`/`o` 的英文；改用 **`/tools expand|collapse`**。
- **工具 registry**：按职责拆分 `tool_*.go` / `fs_*.go`；`call_purpose` 注入与 schema 整理。

### 修复

- **Go TUI**：工具审批结束后 **输入栏上移**（HITL 退出未 `relayout` viewport）。
- **Python TUI**：`--show-reasoning` / `/reasoning` 实际过滤 reasoning SSE 事件。

（Git **tag**：`v0.3.0`。）

## [0.2.22] - 2026-06-11

**0.x 预览**：Linux `install.sh` 重装/升级前自动停止旧 Node。

### 修复

- **Linux `install.sh`**：安装前若目标 `PREFIX` 下存在 `.runtime/node.pid`，优先 `dagents node shutdown` 停止旧进程，失败则按 pid 发 TERM/KILL，避免覆盖二进制时 Node 仍占用 `.runtime`。

（Git **tag**：`v0.2.22`。）

## [0.2.21] - 2026-06-11

**0.x 预览**：Windows `dagents node` 与 Linux 对齐；TUI 行号方框与 transcript 控制符修复。

### 变更

- **Windows `dagents.cmd`**：`dagents node` 默认**后台**启动并等待 probe；新增 **`shutdown`/`restart`**、`--foreground`、`--no-wait`；写入 `.runtime\node.pid`；安装包快捷方式同步调整。

### 修复

- **`read_file` 行号**：由 `N\t` 改为空格对齐，避免 Windows TUI 将制表符显示为方框压住正文。
- **Go TUI transcript**：展开 `\t`、剥离 C0 控制符（含 `\x1e`），修复消息行首方框遮挡文字。

（Git **tag**：`v0.2.21`。）

## [0.2.20] - 2026-06-11

**0.x 预览**：Windows 文件工具 GBK 写盘、jsonl/html 可读、Go TUI `/context` 中文折行修复。

### 新增

- **FS 工具可读后缀**：`read_file` / `grep_*` / `search_replace` 等支持 **`.jsonl`**、**`.html`**。

### 修复

- **Windows `write_file` GBK 编码**：GBK 无法表示的 Unicode 时回退 **GB18030**，仍失败则按 rune 替换 `?`，避免 `rune not supported by encoding`。
- **Go TUI `/context`**：`system_prompt` / `recent_messages` 折行改用显示宽度（`runewidth`），修复中文等多字节字符尾端乱码。

（Git **tag**：`v0.2.20`。）

## [0.2.19] - 2026-06-11

**0.x 预览**：Linux `dagents` 启动与路径修复（任意目录执行、`set -u` 兼容）。

### 修复

- **Linux `dagents` 工作目录**：启动前 `cd` 到安装根（对齐 Windows `pushd`），`fs_root: ./.runtime` 从任意目录执行 `dagents node` 时读写正确的 `.runtime/`（含 `prompt_context/`、`memory/`）。
- **Linux `dagents tui/chat`**：`set -u` 下无额外参数时不再展开空 `PARSED_ARGS` 数组（`unbound variable`）。

（Git **tag**：`v0.2.19`。）

## [0.2.18] - 2026-06-11

**0.x 预览**：Manage 控制面 M0+M1、Register Center Phase 1、Go Node 自动注册与 Console 目录；Go TUI 体验与 Linux `dagents node` 生命周期增强。

### 新增

- **Manage 服务（M0+M1）**：新建 `manage/` 与 `run_manage.py`；Platform + Registry；**Console** `/console/` 展示已注册 Node 列表与状态。
- **Go Node → Manage 自动注册**：`manage.enabled` 时周期 register/heartbeat/deregister；`manage.registration.base_url` 独立上报可达地址；Header `x-dagents-agent-id`；`discovery_group` 由 Manage **`PATCH /v1/registry/agents/{id}/groups`** 分配。
- **Register Center Phase 1（P1.1–P1.3）**：扩展 Agent 登记模型（owner/team/tools/skills/risk_level 等）；派生 `online`/`offline`/`expired` 状态与 offline grace；admin 全局列表（分页/筛选）、`REGISTER_CENTER_TOKENS` 角色鉴权、`GET /v1/admin/audit` 与 `GET /v1/admin/a2a/recent`；relay/broadcast 仅投递 online Agent（离线 `409`）与 `X-DAgents-Trace-Id`。
- **Agent Directory UI（P1.5）**：Register Center 内置 **`/ui/`** 只读目录页（筛选、分页、详情抽屉、A2A 摘要；token 经 sessionStorage 或 `?token=` 注入）。
- **Go 全屏 TUI 体验增强**（`client/internal/tui/full/`、`shared/`）：
  - **`/context` 可滚动**：viewport 渲染 + PgUp/PgDn/↑/↓；context 模式隐藏输入区；resize 不覆盖 context 内容。
  - **启动欢迎面板**、状态栏 turn/审批提示、**`/context` 面板化**（`FormatSessionContextPanel`）、policy 决策档位着色。
  - **工具结果键盘展开/收起**：`o`/`c`、`/tools expand`/`collapse`；`ToolBlockRegistry` + preview/detail 行折叠。
  - **等待态行**：`prefilling` / `thinking` / `compression` 秒级刷新；流式 viewport **60ms debounce**。
  - **工具执行耗时**：pending 占位行每秒刷新（如 `▶ 调用 bash(…) … 3s`）。
  - **审批区动态高度**；无选项 **追问** 在输入框上方显示问题摘要。
  - 退出时打印 **`dagents-client tui --session <id>`** 恢复提示（`/quit`、Ctrl+C）。
- **Linux `dagents node` 生命周期**（`packaging/linux/dagents`）：
  - **`dagents node`** 默认 **后台启动**并等待 probe 就绪；写入 `.runtime/node.pid`。
  - **`dagents node shutdown`**（`stop`）与 **`dagents node restart`**。
  - **`--foreground`** / **`--no-wait`**（兼容旧 `--background` 即发即走）。

### 变更

- **Manage Console**：未配置 token 时开放模式可直接浏览全部 Node；统计请求 `page_size` 上限对齐 API（200）。
- **Go TUI 键盘优先**：help 与交互说明以键位为主；不新增鼠标点击类能力。
- **`dagents node` 默认行为**：由前台 `exec` 改为后台 + 就绪探测；前台需显式 `--foreground`。

### 修复

- **Linux `dagents` 符号链接安装**：通过 `/usr/local/bin/dagents` 调用时正确解析 `DAGENTS_HOME`（修复 `bin/dagents-node` 路径落在 `BIN_DIR` 的问题）。

（Git **tag**：`v0.2.18`。）

## [0.2.17] - 2026-06-10

**0.x 预览**：在 **v0.2.16** 基础上修复 **async 回灌清掉 pending HITL**，并为 **FS 文件工具** 增加磁盘编码支持。

### 新增

- **`tools.file_encoding`** 与 FS 工具可选 **`encoding`**（`utf-8` / `gbk` / `gb18030`）：`read_file`、`write_file`、`search_replace`、`grep_file`、`grep_files` 读写磁盘时按编码转码；默认 Windows→gbk，其它→utf-8；单次参数优先于 config。

### 修复

- **async 工具回灌保留 pending HITL**（[#25](https://github.com/DGS-ai-team/DAgents/issues/25)）：后台 job 完成时不再因 `applyStepOutcome` 清掉等待中的工具审批，避免 resume `409 no_pending_hitl`。

（Git **tag**：`v0.2.17`。）

## [0.2.16] - 2026-06-07

**0.x 预览**：在 **v0.2.15** 基础上新增 **策略（policy）API 与 TUI**、**trigger 会话目标审批** 与 **`/triggers` 命令**，并修复 **TUI 输入崩溃** 与 **Windows `--withnode` 启动**。

### 新增

- **`GET/PUT /v1/policy`**：工具与 shell（bash/cmd/powershell）策略读写；`policy` 包支持 ModeDeny、DecideTool、txt 存储与 Orchestrator 热更新。
- **Go / Python TUI `/policy`**：查看与 `set` 子命令；分页展示工具与 shell 规则（Python `policy_view`）。
- **Go / Python TUI `/triggers`**：列出调度 trigger 及下次执行时间。
- **Trigger 会话目标**：审批 payload 可指定 `session_id`；HITL resume 与 `trigger_session` 贯通 Node / Client。

### 变更

- **System prompt**：精简 turn 构建逻辑与测试（`node/internal/turn/prompt.go`）。
- **配置与 `fs_root`**：收敛默认沙箱路径说明；示例 `config.yaml` / `policy.example.yaml` 对齐。

### 修复

- **Python TUI 输入崩溃**：`PromptTextArea` 改用 `on_text_area_changed`，移除无效 `super()` 调用。
- **Python TUI `/policy` Rich 报错**：Markdown 中 `[/]` 改为 `[ / ]` 避免被解析为标签。
- **Windows `dagents chat --withnode`**：`-config` 参数顺序、`/D` 工作目录与失败时 probe / `node.err.log` 提示（`packaging/windows/dagents.cmd`）。
- **Linux `dagents` probe**：健康检查参数顺序与 Windows 对齐。

（Git **tag**：`v0.2.16`。）

## [0.2.15] - 2026-06-10

**0.x 预览**：在 **v0.2.14** 基础上修复 **Python TUI usage/滚动**、增强 **工具结果展开** 与 **Go TUI usage 展示**。

### 修复

- **Python TUI usage 折行**：assistant 完成态 usage 改用 Rich `Align.right` 独占一行，修复 `think 42` 等被 `overflow=fold` 拆开。
- **Python TUI 滚轮崩溃**：移除 `on_mouse_scroll_*` + `event.widget.id`；改由 `TranscriptLog.watch_scroll_y` 维护 follow-tail。
- **Python TUI USAGE 晚到**：`_apply_round_usage` retroactive 重写最近已完成 assistant 块（对齐 Go `ApplyRoundUsage`）。

### 变更

- **Python TUI 滚动跟随**：去掉 `_log_write_block` / `_write_assistant_block` 内冗余 follow-tail 兜底，统一由 `scroll_y` 监听驱动。
- **Python TUI 工具结果展开**：展开时同时展示输入与输出（bash / search_replace / 通用工具）。
- **Go TUI usage 展示**：transcript usage 独占一行右对齐；input strip 使用 `FormatInputStripLine`（runewidth 布局，窄屏截断左侧）。

（Git **tag**：`v0.2.15`。）

## [0.2.14] - 2026-06-07

**0.x 预览**：在 **v0.2.13** 基础上增强 **TUI 斜杠命令展示与滚动体验**，并新增 **CentOS 7 特性导览** 落地案例。

### 新增

- **`cases/centos7-feature-tour/`**：CentOS 7 容器 + 静态 Node；README 以 **TUI 输入/观察** 为主（Mock 无需 API Key）；`scripts/verify.sh` 供 CI/运维冒烟。

### 变更

- **Go 全屏 TUI 命令面板**：`/status`、`/sessions`、`/skill`、`/help`、`/children` 使用结构化 system panel（标题 + 分区/键值/高亮），不再把纯文本丢进 transcript。
- **Go / Python TUI 滚动跟随**：显式 **`viewportFollowTail` / `_transcript_follow_tail`**；流式输出、审批等待、工具详情展开（Python 点击）时，用户上滚后不再被强制拽到底；滚回底部或发送消息恢复跟随。Go 支持 **PgUp/PgDn**、输入框为空时 **↑/↓**、鼠标滚轮；流式 **partial** 实时进 viewport。
- **Python TUI 命令面板**：`/status`、`/session`（含 `/sessions` 别名）、`/skill`、`/help`、`/children` 使用 Rich **Panel** 边框展示。
- **案例目录**：可复现场景迁至仓库根 **`cases/`**（索引仍链到 [`docs/cases/README.md`](docs/cases/README.md)）。

（Git **tag**：`v0.2.14`。）

## [0.2.13] - 2026-06-07

**0.x 预览**：在 **v0.2.12** 基础上取消发布包内置 OfficeCLI、对齐 **`fs_root` 默认沙箱**、精简工具输出，并增强 skills 与 TUI 工具展示。

### 变更

- **不再内置 OfficeCLI**：移除 Release 打包时的 `vendor_officecli.sh` 集成；新增 **[`packaging/runtime/RECOMMENDED_CLI_TOOLS.md`](packaging/runtime/RECOMMENDED_CLI_TOOLS.md)** 推荐 CLI 清单（含 OfficeCLI 自行安装说明）。
- **`fs_root` 默认为 `./.runtime`**：文件工具沙箱与运行时目录对齐；system prompt 工作区说明改为相对 FS_ROOT 的路径（`skills/`、`data/` 等）。
- **`search_replace` 输出精简**：成功且单次单行替换仅返回元数据；多处或多行替换附限流局部预览；移除整文件逐行 diff。
- **skills tool 结果精简**：`load_skills` / `unload_skills` / `clear_skills` 的 tool 结果 JSON 不再含 `available_skills`（仅 `action` + `loaded_skills`）。
- **skills Catalog mtime 缓存**：`List()` 在各 `SKILL.md` 未变时复用内存列表，减少每步 prompt / `load_skills` 的重复读盘。
- **工具 `call_purpose`**：内置工具 schema 注入 `call_purpose`；Go / Python TUI 与审批首行优先展示 `tool(purpose)` 短标题；执行前从 arguments 剥离该字段。
- **`write-skill` 元数据**：修复 description 缺失时显示 `<nil>`；打包 skill 路径说明与 `fs_root` 默认对齐。

（Git **tag**：`v0.2.13`。）

## [0.2.12] - 2026-06-08

**0.x 预览**：修复 **v0.2.11** Windows 发布包组装时 OfficeCLI skills 解压失败。

### 修复

- **`vendor_officecli.sh`**：解 skills  tarball 时一并提取根目录 **`SKILL.md`**（`skills/officecli/SKILL.md` 为 symlink）；复制 skills 时使用 **`cp -RL`** 展开 symlink，避免 Windows CI / zip 分发出现断链。

（Git **tag**：`v0.2.12`。）

## [0.2.11] - 2026-06-08

**0.x 预览**：在 **v0.2.10** 基础上增加 **手动压缩与 context 诊断**、**预编译包 Node 自启动**、**Windows 内置 OfficeCLI**，并改进 Go TUI 滚动体验。

### 新增

- **`POST /v1/sessions/{id}/compress`**：手动触发一次阻塞压缩（忽略 token 阈值）；turn 进行中返回 `409 turn_busy`。
- **Go / Python TUI `/compress`**：调用上述 API；已有压缩任务进行中时展示 `in_progress` 及 `trigger_level` 等字段，不重复执行。
- **`GET /v1/sessions/{id}/context`** 与 **`/context`**：响应/展示 **`system_prompt`**（与 turn 构建逻辑一致）。
- **预编译包 Node 自启动**：Linux **`dagents`**、Windows **`dagents.cmd`** 支持 **`--withnode`**（Client 前探活并后台启动 Node）与 **`node --background`**（日志 `.runtime/logs/node.log`）。
- **Windows 发布包内置 OfficeCLI**：[`scripts/ci/vendor_officecli.sh`](scripts/ci/vendor_officecli.sh) 打入 **`.runtime/scripts/officecli.exe`** 与 **`.runtime/skills/officecli*`**（上游 **[iOfficeAI/OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)**，AGPL-3.0）；Linux tarball **不含** OfficeCLI。
- **`.runtime/scripts` 进 PATH**：Linux **`install.sh`** / systemd 服务、Windows 安装包，便于 Agent 与用户调用扩展 CLI。

### 变更

- **压缩协调器**：silent / blocking / 手动压缩共用 **`compressionTask`** 跟踪；手动压缩遇进行中任务返回 **`in_progress`**。
- **Go 全屏 TUI**：transcript 上滚后新输出不再强制跳底；支持鼠标滚轮；用户发消息时仍 **`syncViewportFollow`** 贴底。

### 修复

- **`contextView` 死锁**：持 session 锁时调用 `SystemPromptForSession` 改为先 unlock 再构建 system prompt。
- **Go TUI 窗口缩放**：resize 时保留 viewport 阅读位置（贴底时仍跟随）。

（Git **tag**：`v0.2.11`。）

## [0.2.10] - 2026-06-07

**0.x 预览**：在 **v0.2.9** 基础上完善 **skills 工具 schema 描述**，引导模型在匹配任务时主动调用 `load_skills`。

### 变更

- **`load_skills` / `unload_skills` / `clear_skills` 工具描述**：集中说明目录仅含元数据、任务匹配 description 时需先加载、整组替换与清空语义、与 `available_skills` 的名称对齐规则；用法不写进 system prompt，由工具 schema 承载。

（Git **tag**：`v0.2.10`。）

## [0.2.9] - 2026-06-07

**0.x 预览**：在 **v0.2.8** 基础上修复 **打断工具后重复 tool 消息** 导致的 LLM 400。

### 修复

- **打断 pending 工具去重**：`InterruptPending` 与 `RepairUnrespondedToolCalls` 共用 `insertMissingToolResponsesAfterAssistant`，按 `tool_call_id` 跳过已有 tool 响应，避免同一 call 写入两条「用户需要补充信息，打断了工具执行。」后 LLM 报 `Messages with role 'tool' must be a response to a preceding message with 'tool_calls'`。
- **idle cancel 清 pending**：`cancelTurn` 在 idle 下 repair 补写 tool 后清除 `pending`，防止后续用户消息再次 interrupt 重复补位。

（Git **tag**：`v0.2.9`。）

## [0.2.8] - 2026-06-08

**0.x 预览**：在 **v0.2.7** 基础上增强 **TUI 展示**、修复 **非法 tool 序列**、统一 **Skill 元数据格式**。

### 新增

- **assistant usage 右对齐**：Textual 与 Go 全屏 TUI 在末行放得下时同行右对齐，否则独占下一行仍右对齐；Go transcript 用 `\x1e` 分隔正文与 usage，展示层上色。
- **消息圆点分色**：用户 / assistant / reasoning / 工具 / 状态等类型使用不同颜色圆点。
- **非法 tool 序列修复**：`RepairUnrespondedToolCalls` 在 LLM 调用前、新 user 消息与 idle cancel 时补写 interrupted tool 结果，避免 `tool_calls` 后缺 tool 消息导致 400。
- **Skill 标准 frontmatter**：`name` + `description`；目录下全部 `SKILL.md` 参与元数据扫描（移除 per-skill `enabled`）。

### 变更

- **多工具审批 UI**：仅当前待审工具展示代码详情；`bash(...)` 标题压平参数换行。
- **工具等待耗时**：与 prefilling 一致按整数秒递增；审批结束后重置执行计时。
- **Esc 取消 HITL**：审批 Esc 提交全拒绝 resume；用户询问 Esc 提交 `cancelled` resume（不再只清本地队列）。
- **内置 `write-skill`**：SKILL.md 改为标准 `name` + `description` 示例。

### 修复

- 用户打断或取消后 history 尾部 assistant+tool_calls 无 tool 响应时，下轮 LLM 请求失败的问题。

（Git **tag**：`v0.2.8`。）

## [0.2.7] - 2026-06-08

**0.x 预览**：在 **v0.2.6** 基础上增加 **`bash_run` 输出压缩**、**tool/usage SSE 展示增强** 与 **双 TUI inline token 用量**。

### 新增

- **`bash_run` 输出压缩（P0）**：L1 ANSI/空行/重复行清洗 + rune 上限截断；配置项 `tools.bash_compress`（`enabled`、`max_output_chars`、`max_output_chars_stderr`）。
- **压缩统计 SSE**：`tool_result` 附加 `output_compress_saved_pct` / `output_compress_raw_runes` / `output_compress_out_runes`（仅 bash 且有节省时）；Go / Textual 工具行展示 `· -N%`。
- **usage 单轮 + turn 累计**：SSE `usage` 含顶层 turn 累计与 `round_*` 单轮字段、`llm_step`；input strip 展示 turn 累计；assistant 块末 inline 展示单轮用量（浅灰）。

### 变更

- **`bash_run` tool 正文精简**：`[BASH_RESULT] exit=N` + `--- STDOUT ---` / `--- STDERR ---`；压缩元数据不再写入 LLM context。
- **Client**：`grep`/`bash` 工具行适配新输出格式；inline usage 仅挂在 assistant 后（不在 thinking/reasoning 后）。

（Git **tag**：`v0.2.7`。）

## [0.2.6] - 2026-06-08

**0.x 预览**：在 **v0.2.5** 基础上拆分 **文件发现** 与 **内容检索** 工具，降低 `search_file` 名实不符带来的模型误用。

### 新增

- **`glob_files`**：在 `directory` 下按 glob（含 `**` 递归）列举匹配路径，分页返回，不读文件内容。
- **`grep_file`**：单文件行内容检索（正则/字面量、上下文、`index_offset` 翻页）；替代原 LLM 可见的 `search_file`。
- **`grep_files`**：目录树内先 `glob_pattern` 筛文件再逐行检索，跨文件命中分页（`hit_offset` / `max_hits` / `max_files`）。
- **共用实现**：`fs_glob.go`、`grep_shared.go`；glob 匹配依赖 `github.com/bmatcuk/doublestar/v4`。

### 变更

- **LLM 工具列表**：注册 `glob_files`、`grep_file`、`grep_files`；`search_file` 仍保留 handler 别名（兼容旧调用），不再出现在 `Definitions()`。
- **子 Agent 默认工具**：`read_file`、`glob_files`、`grep_file`、`bash_run`；父 Agent 可下放列表含 `grep_files`。
- **审批策略**：新只读工具默认 `never`（`policy` + `tool.approval.txt`）。
- **工具描述**：各工具 `description` 仅描述自身能力，不交叉引用其它工具名。

### 移除

- **`search_file.go`**：逻辑并入 `grep_file` / `grep_shared`。

（Git **tag**：`v0.2.6`。）

## [0.2.5] - 2026-06-08

**0.x 预览**：在 **v0.2.4** 基础上收敛 **LLM 厂商适配**、**turn/session 模块边界** 与 **流式中断 history** 行为。

### 新增

- **`llm/messageutil.go`**：`CloneMessage`、`EstimateMessageTokens`（含 `reasoning_content`）、DeepSeek/JSONL payload 辅助。
- **Turn 模块拆分**：`tool_router.go`（工具分流与执行）、`cancel_partial.go`（流式 cancel 部分 assistant 落库）、`history_write.go`（history 写入）。
- **`session/runtime_turn.go`**：`runTurnStep` / `finishTurnIdle` 统一四路 handler 脚手架。
- **包文档**：`llm/`、`store/`、`api/`、`queue/`、`stream/`、`hitl/` 的 `README.md`。

### 变更

- **`MessageAdapter`**：新增 `MarshalChatRequestMessages`；DeepSeek 出站序列化集中在 `provider_deepseek.go`；`openai.go` 不再按厂商名分支。
- **DeepSeek**：带 `tool_calls` 的 assistant 允许空 `reasoning_content`（出站强制键存在）；不再从最近 assistant 继承 reasoning。
- **Token 估算**：压缩触发与 `GET /context` 共用 `llm.EstimateMessageTokens`。
- **JSONL**：`messageToJournalPayload` 委托 `llm.MessageToJournalPayload`。

### 修复

- **流式 cancel**：`persistCancelledStream` 保留已流式输出的 assistant；未响应 `tool_calls` 补 interrupted tool 消息，保证下轮合法序列。
- **子 Agent 结算**：`finishTurnIdle` 在 `applyStepOutcome` 之后调用，修复 `tryCompleteChildIfIdle` 看不到最终 assistant 的时序问题。

### 移除

- 死代码：`hitl/waiter.go`、`session.handleMessage`、`provider` 未使用的 reasoning 继承 helper。

（Git **tag**：`v0.2.5`。）

## [0.2.4] - 2026-06-08

**0.x 预览**：在 **v0.2.3** 基础上增加 **LLM 厂商适配层**、**usage / reasoning token 统计**，并统一 **双 TUI transcript 排版**。

### 新增

- **LLM Provider 适配**（`node/internal/llm/`）：`llm.provider` 选择 `MessageAdapter`（`openai` / `deepseek`）；DeepSeek 自动注入 `thinking.enabled` 与 `reasoning_content` 出站校验；`RequestExtra` 合并进 Chat Completions 请求体。
- **Usage 归一化**：`usage.go` 兼容 OpenAI `cached_tokens` 与 DeepSeek `prompt_cache_hit_tokens`；解析 `completion_tokens_details.reasoning_tokens`、顶层 `reasoning_tokens` 及 `completion_token_details` 别名；SSE `usage` 含 `prompt_cache_hit_rate` 与 `reasoning_tokens`。
- **Turn 内 usage 累加**：工具循环多轮 LLM 调用在单条 user turn 内累加 token 统计后再发布 SSE。

### 变更

- **`reasoning_content` 收敛至 LLM 层**：history 不再做 assistant 规范化；由 provider adapter 在存储与出站前处理。
- **双 TUI transcript**：圆点列统一为 Rich `Table.grid` 布局；prefilling/thinking、assistant↔tool、tool 批次↔下一段、human message 与上下文的空行规则一致化。
- **配置示例**：`packaging/agent-client/config.example.yaml` 补充 `llm.provider` 说明。

### 修复

- **Go / Textual Client**：input strip 展示 cache hit 与 `· think N`（`reasoning_tokens`）；`completion_tokens_details` 嵌套字段兜底解析。
- **Transcript 空白行**：流式 assistant 空 partial 不落行；`tool_result` 后不再重复插入空行；Go REPL/full 跳过空 assistant delta。

（Git **tag**：`v0.2.4`。）

## [0.2.3] - 2026-06-08

**0.x 预览**：在 **v0.2.2** 基础上增加 **Windows 安装包**、**Linux 安装脚本**，并修复 **Windows GBK 环境下 shell 工具输出乱码**；强化子 Agent 提示词与双 TUI 展示。

### 新增

- **Windows 安装包（Release / manual-package）**：Inno Setup 生成 `dagents-local-assistant-windows-amd64-installer-*.exe`；包内 `dagents.cmd` 统一入口（`node` / `chat` / `tui` / `register-center`）。
- **Linux 分发**：tar.gz 根目录含 **`dagents`** 启动脚本与 **`install.sh`**（用户级 `~/.local/share/dagents` 或系统级 `/opt/dagents`，配置 `PATH` / `DAGENTS_HOME`）。
- **Shell 工具输出解码**：`config.yaml` → **`tools.bash_output_encoding`**（如 `gbk` / `utf-8`）；未配置时 Windows **cmd/powershell 默认 gbk**，**bash 默认 utf-8**，解码后以 UTF-8 交给 LLM。
- **子 Agent**：Orchestrator 注入 system prompt builder；`create_temporary_agent` 支持 **`skill_names`** 预加载；子 runtime **`BuildChildSystemPrompt`** 含已加载 skill 正文。

### 变更

- **`.env.example`**：仅保留 Register Center 与 Python CLI 仍读取的项；Node/LLM 主配置在 **`config.yaml`**。
- **双 TUI**：友好格式化 `wait_temporary_agents` 等工具结果；`wait_temporary_agents` 后抑制与工具结果重复的 `temporary_agent_completed` lifecycle 行。
- **打包文档**：`packaging/linux/`、`packaging/windows/` 与 CI assemble 说明更新。

### 修复

- **Release CI（Windows）**：Inno Setup 编译时 Git Bash 将 `/DMyAppVersion=…` 误转为 `D:\…` 导致 `ISCC` 失败；改用 `//D` 前缀。
- **Windows 中文环境**：`bash_run` 捕获 GBK 字节流时 Agent 此前收到乱码；现按配置/平台解码后再写入 `[BASH_RESULT]`。

（Git **tag**：`v0.2.3`。）

## [0.2.2] - 2026-06-07

**0.x 预览**：在 **v0.2.1** 代码基础上对齐 `/health` 与 Client 版本号为 **0.2.2**；**本版重点修复工具审批（HITL）相关缺陷**。

### 修复

- **工具审批（HITL）**：
  - **Go full TUI**：父 Agent 与子临时 Agent 并发出现 `approval_required` 时，审批队列曾按单一槽位去重，导致后到的审批覆盖先到、或用户批准后 `resume` 无法匹配正确 `tool_call_id`。现以 **`ApprovalQueueKey`** 分桶（父：`approval_id`；子：`child_session_id`），同键仅保留最新一条，不同 Agent 的审批可并行排队。
  - **resume 路由**：`SubmitResume` 载荷携带 `approval_id` / `child_session_id`，与 Node pending HITL 对齐，避免审批结果投递到错误会话或静默失败。
  - **Node**：启用临时 Agent 时，父 session 的 `resume` 曾被重复入队，导致队列积压、审批无法继续；现保证单次入队（见 `TestEnqueueResumeParentDoesNotDoubleEnqueue`）。
  - **Textual TUI（`dagents chat`）**：审批队列与用户追问（`ask_user_information`）展示与 Go Client 语义对齐，避免子 Agent 审批与父审批互相顶替。

### 变更

- **临时 Agent**：工具与 SSE 统一为 **temporary agent** 命名；子 runtime 禁止 `load_skills` / `unload_skills` / `clear_skills`；`ActiveAgent` 等内部命名整理。
- **文档**：Python Agent 运行时迁入 `docs/archive/python-agent-runtime/`；新增 [`docs/architecture/go-node-internals.md`](docs/architecture/go-node-internals.md) 与 `node/internal/session`、`turn` 包 README。

（Git **tag**：`v0.2.2`。）

## [0.2.1] - 2026-06-07

中间 tag，**未** bump 代码内 `Version` 常量；变更已包含在 **0.2.2**。主要内容：临时 Agent 重构与文档归档（见上）。

（Git **tag**：`v0.2.1`。）

## [0.2.0] - 2026-05-30

**0.x 预览**：**Go Agent Node + Client** 成为唯一 Agent 运行时；本地助手与终端交互以 Go 栈为主线。Python 保留 **Textual TUI Client**（`dagents chat`）与 **Register Center**，**Python FastAPI Agent API 已从仓库移除**（原 `app/harness/`、`run_agent_api.py`）。

### 新增

#### Go Agent Node（`node/`）

- **HTTP/SSE API**：会话创建/恢复、消息提交、`/resume`、流式事件、`/cancel`、context/skills/child-agents 等（契约见 `docs/architecture/agent-node-api.md`）。
- **Turn 编排**：OpenAI 兼容 LLM、工具循环、审批暂停、`user_information` 询问、上下文压缩（blocking/silent SSE）。
- **工具**：`bash_run`、`read_file` / `write_file` / `search_replace`、`load_skills` / `unload_skills`、trigger 系列、子 Agent 工具（`spawn_child_agent` 等）。
- **Triggers**：`interval`、`fire_at`、日历 **`schedule`**（含 `cmd` 门控）；SQLite/JSON 持久化。
- **Session**：SQLite 消息持久化；多 session 队列与活跃 turn 管理。
- **Policy / Skills**：本地 policy profile；按 session 加载 skill 元数据。
- **Usage 事件**：SSE `usage` 含 prompt/completion、cache hit/miss、`prompt_tokens_details`（供 Client input strip）。

#### Go Client（`client/`）

- **bubbletea 全屏 TUI**（`tui`，默认）：上输出 / 下输入；SSE 断线按 `Last-Event-ID` 重连。
- **行模式 REPL**（`tui --plain` / `TERM=dumb`）：老 SSH / RHEL6 兜底；turn 期间独占 stdin，完整走通 HITL。
- **HITL（full）**：逐工具审批（↑/↓、Space、Enter）；选项式 `ask_user_information`；子 Agent 审批标题区分。
- **展示友好化**：`[tool] ▶ 调用 bash(…)` / `✓ bash 完成`；审批展示参数/原因/风险；input strip `↑prompt ↓completion · hit N`。
- **斜杠命令**：`/context`、`/skill`、`/children`、`/cancel`、`/tools verbose|brief`、`/reasoning on|off` 等。
- **探测与一次性 chat**：`probe`、`chat "消息"`（非交互）。

#### 共用配置（`shared/config/`、`packaging/agent-client/`）

- **YAML 同包配置**：`agent_id`、`listen`、`local.endpoint`、`llm`、`data_dir` 等；Node 与双 Client 共用。
- **本地助手打包**：`dagents-local-assistant-*` tarball/zip（Go Node + Go Client + Textual CLI + `config.example.yaml` + 启动脚本）。

#### Python Textual TUI（`app/cli/`）

- **`dagents chat`**：连 **Go Node**（非 Python API）；RichLog 流式输出、非阻塞 HITL、子 Agent 过滤与状态条。
- **共用 YAML**：`--config` / `DAGENTS_CONFIG` 与 Go Client 对齐；`show session` / `delete session`。
- **Usage strip**：消费 SSE `usage`，显示上下行 token 与 cache hit（与 Go Client 语义一致）。

#### Register Center（`register_center/`）

- **持久化**：可选 JSON 文件存储；TTL  prune。
- **A2A 指标**：relay/broadcast Prometheus 计数（`register_center/metrics.py`）。
- **安全**：共享 token 校验；relay resume 转发。

#### 文档与工程

- **文档重组**：原 `docs/architecture-v2/` 迁入 `docs/architecture/`、`docs/design/`、`docs/future/`、`docs/archive/`。
- **N7 部分**：`go-node-compatibility.md`、Go 静态构建脚本、RHEL6 SysV init、Release CI Go tarball。
- **子 Agent 设计**：`docs/architecture/child-agent-tools.md`。

### 变更

- **运行时主线**：本地助手默认 `go run ./node/cmd/dagents-node` + `dagents chat` 或 `dagents-client tui`；不再提供 Python Agent API 进程。
- **`run_dev_stack.py`**：仅启动 Register Center（原 API + RC 联调入口简化）。
- **`dagents` CLI**：移除 `serve` / `api` 子命令（原后台 `dagents-api`）；保留 `chat`、`show`、`delete`、`register-center`。
- **CI**：PR 跑 Go `node/` / `client/` 单测 + Python CLI/RC 单测；移除 Python OpenAPI 导出步骤；`dagents-cli` 构建改用 Rocky Linux 8。
- **`requirements.txt`**：移除 `openai`、`APScheduler` 等仅 Agent API 栈依赖。
- **Prometheus（Register Center）**：A2A 指标从 `app/observability/` 迁至 `register_center/metrics.py`。

### 移除

- **Python FastAPI Agent 运行时**：`app/harness/`、`app/core/`、`app/context/`、`app/schemas/`、`app/observability/`、`run_agent_api.py`、`export_openapi_schema.py`。
- **Legacy 打包**：`packaging/linux/dagents-backend.*`、旧全栈 `startup_scripts` 中的 `dagents-api` 启动脚本。
- **辅助脚本**：`scripts/query_session_sqlite.py`、`scripts/migrate_runtime_layout.py`、`scripts/ci/export_openapi_for_frontend.py`。
- **Python Node 单测**：`test_agent_service*.py`、`test_api_app.py`、`test_main_agent_*.py` 等约 20+ 文件。
- **Windows 托盘启动器**（`scripts/windows/tray_launcher.py` 等）。

### 修复

- **HITL**：Python/Go Client 与 Node 间 `tool_call_id` 同步；新 `approval_required` 替换 stale 队列项。
- **Go full TUI**：textarea 默认 Focus；placeholder UTF-8 首字节乱码（`> 输入消息...`）。
- **Go plain REPL**：发消息后等待 `done` 再显示 `dagents>`，避免与 HITL 抢 stdin；`done` 正确收尾 assistant 行。
- **Trigger 调度与日志**：fire 路径与 observability 对齐（Go Node）。

### 已知限制（0.2.0）

- **N7 真机验收**：RHEL6 / Windows Server 2012 等待测清单见 `docs/architecture/rhel6-acceptance-checklist.md`。
- **plain REPL**：工具审批仅 y/N 全批/全拒；无 full TUI 的逐项勾选；回合等待期间不可用 `/cancel`。
- **A2A 工具**：随 Python Agent API 移除；Register Center relay/broadcast 仍可用，Agent 侧 A2A 需后续在 Go/Manage 落地。
- **历史文档**：`docs/api-reference.md`、`docs/architecture/python-runtime.md` 描述已删除的 Python API，仅作参考；现行契约见 `docs/architecture/agent-node-api.md`。
- **Web UI（DAgentsUI）**：独立前端仓库 **尚未适配 v0.2.0**，仍基于旧 Python Agent API / OpenAPI；浏览器 Client 暂不可用，请使用终端 TUI。
- **0.x 预览**：破坏性变更在 **1.0** 前仍可能出现。

（Git **tag**：`v0.2.0`。）

---

## [0.1.0] - 2026-05-12

首个对外标记版本（**0.x 预览**）：核心 Agent API、可选 SQLite 会话持久化、SSE 流式事件、Prometheus 指标、Register Center 与配套文档/单测基线。以下 **变更 / 文档 / 仓库维护** 与首版同批交付（尚未单独发补丁版号时，仍以 **`v0.1.0`** 与 Git tag 对齐）。

### 包含（概要）

- FastAPI Agent 服务：会话、消息提交、resume、SSE、取消 turn、会话释放等（详见 `app/harness/api/README.md`）。
- 本地联调脚本（`run_dev_stack.py` 等）；仓库根其它 `run_*.py` 入口仍在演进，交互能力不以变更记录为对外承诺。
- OpenAI 兼容运行时：工具调用、审批流、异步工具结果回灌、上下文压缩相关能力。
- `register_center/`：Agent 登记与发现（内存实现）。
- 配置：`app/config/settings.py`、`.env.example`；可观测：`/metrics`（可关）。
- 单元测试：`tests/` 下 `unittest discover`；可选联网冒烟见 `tests/integration/`。
- `docs/`：对外技术文档除 **`api-reference.md`**、**`prometheus-metrics.md`**、**`architecture-and-flows.md`**、**`agent-input-output.md`**、**`context-compression-and-state.md`** 外，另含 **`agent-turn-loop.md`**、**`a2a-and-register-center.md`**、**`built-in-tools.md`**、**`roadmap.md`** 与 **`cases/`** 案例目录索引等；索引见 **`docs/README.md`**；其余以代码与 **`app/**/README.md`** 为准。

### 变更

- **Prometheus（LLM token）**：`dagents_llm_*` token 指标由 **Gauge `set`（末次快照）** 改为 **Counter `inc`（进程内累计）**，指标名增加 **`_total`** 后缀（例如 **`dagents_llm_prompt_tokens_total`**）；监控面板的 PromQL 与告警需改用新指标名。语义约定：每次流式 **`usage`** 分片表示 **当前 completion 请求** 的消耗；若网关上报账号级终身累计会导致重复累加。
- **系统提示侧车**：运行时从 **`<运行根>/.runtime/prompt_context/`** 读取 **`soul.md` / `user.md` / `custom.md`**；若缺失则由 **`prompt.py`** 创建 **空 UTF-8 文件**（不覆盖已有文件），**不从其它路径拷贝文案**。**`packaging/runtime/prompt_context/`** 提供 **空文件占位**；**`packaging/runtime/`** 另含 **`scripts/`**、**`data/`** 等目录占位，随发布 zip 并入 **`bundle/.runtime/`**。
- **异步工具**：`AsyncToolResultStore.submit_coroutine` 要求非空 **`client_id`**；`OpenAIConversationContext` 新增进程内 **`sse_client_id`**（由带 **`client_id`** 的入站 `MessageEnvelope` 刷新），异步工具终态回灌的 **`MessageEnvelope.client_id`** 与该通道对齐，保证 SSE 可投递至原客户端。

### 文档

- **`docs/context-compression-and-state.md`**：补充 **SQLite 会话记忆**、侧车 Markdown、**`get_system_prompt`** 拼接顺序；索引见 **`docs/README.md`**。
- 新增 **`docs/agent-turn-loop.md`**：讲解 **队列外层 + `run_turn` 单轮内层**、**`_run_turn_and_maybe_execute_tools`**、**`tool_result` 入队** 与审批 / **`async_tool_result`** 分支。
- 新增 **`docs/a2a-and-register-center.md`**：**A2A（`agent_peer`）** 与 **Register Center**（登记、**`broadcast`/`relay`**、配置与审批 **`resume` 直连** 约束）。
- 新增 **`docs/built-in-tools.md`**：**`get_tools()`** 清单、**`@tool` / `tool()`** 装饰逻辑、**docstring → LLM**、**`parameters` 与 `parse_tool_arguments` → `invoke` 管道**、异步工具与 **`client_id`**、**`host_platform` 未注册** 等。
- 新增 **`docs/roadmap.md`**：**路线图**（已实现能力、待办、已知限制；含 **§3.4** CLI / 子 Agent / A2A 与 Register Center 增强 / 压缩 / 内置记忆书等规划方向）。
- 新增 **`docs/cases/`**：落地案例目录（见 **`docs/cases/README.md`**），用于收录各场景实践与效果供参考。
- **`docs/README.md`**：**`cases`** 入口链接格式修正；**`docs/roadmap.md`**：§3.4 **A2A 优化** 条目补全（广播 SSE、跨 NAT 等表述）。

### 仓库维护

- **`.gitignore`**：增加 **`dist/`**、**`build/`**、**`bundle/`**，避免将 PyInstaller 与本地打包中间产物误提交。
- **`tests/test_agent_service.py`**：懒加载 **`OpenAIImplicitReActRuntime`** 时 patch **`app.core.main_agent.runtime_openai.get_openai_client`**，避免新版 **OpenAI** SDK 在 **`LLM_API_KEY` 为空** 的 CI 环境于 **`AsyncOpenAI(...)`** 构造期抛错，导致 **`_session_consume_loop`** 崩掉、生命周期断言失败。

### 已知限制（0.1.0）

- **HTTP API 单测**未在仓库内全覆盖；CI 以 `requirements.txt` 安装后跑默认 `test_*.py`。
- **`test_agent_service.py`** 在缺少完整依赖（如未安装 `openai`）时相关用例会 **skip**，与 CI 全量安装行为不同。
- 破坏性 API 变更在迈向 **1.0** 前仍可能出现；见 [README.md](README.md) 中「版本与兼容性」。

> **注**：0.1.0 中的 Python FastAPI Agent 运行时已在 **0.2.0** 移除；上文保留作该版本历史记录。

（Git **tag**：`v0.1.0`；请在对应托管平台上创建 **Releases** 并与该 tag 对齐。）
