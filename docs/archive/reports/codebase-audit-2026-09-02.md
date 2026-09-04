# DAgents 全仓库代码审计报告（2026-09-02）

> 这是一份带日期的一次性审计报告，用于记录当前分支的 CI 收口和代码清理候选，不是新的产品契约。后续实现以代码、测试、Schema/OpenAPI 以及 `docs/architecture.md` 和当前设计文档为准。

## 1. 执行摘要

本次审计同时处理了当前 PR 的 CI 失败，并对 Node、Web UI、Manage、共享 Go 模块、桌面 Shell、browser-service、脚本和文档进行了静态梳理。

结论如下：

1. 本轮发现的高置信度未使用私有代码已经删除；当前 Go Staticcheck、Ruff、Pyright 均通过。因此不建议再进行一次性大面积删除。
2. 仓库确实存在三类维护成本：
   - Workgroup 同时保留 AgentRef 与 legacy_member 两套执行模型；
   - Node Web UI 与 Manage Console 重复实现相同的 Workgroup 事件投影。
   - 触发器和消息来源仍同时暴露新字段、旧字段和推断别名。
3. 最明确的低风险清理对象已经完成删除；剩余候选大多是协议级收敛，不能只删除一个别名而留下另一半路径。
4. 旧 Shell、Placement 数据列、A2A 来源枚举、浏览器异步任务和历史消息来源仍承担当前产品职责，不能仅凭名称视为死代码。
5. Python 安全扫描的三个新告警来自 `browser-use==0.13.8` 对 `pypdf==6.14.2` 的精确依赖。当前 CI 通过显式记录这三个上游阻塞项来恢复扫描，同时继续拦截新增漏洞；这不是漏洞修复，必须设置升级触发条件。

## 2. 审计范围与方法

### 2.1 覆盖范围

本次扫描了以下代码和交付面：

| 区域 | 关注点 |
| --- | --- |
| `node/` | Turn、Session、LLM、工具、记忆、Skills、MCP、API、持久化和内嵌 Web UI |
| `node/webui/frontend/` | Agent、Terminal、Workgroup、设置页、工具卡片和状态管理 |
| `manage/` | Registry、Workgroup 后端和 Console |
| `shared/` | 配置、日志、更新和 Workgroup 共享包 |
| `client/`、`desktop/` | 客户端、Tauri Shell 和 Go Shell 兼容实现 |
| `browser-service/` | Python 浏览器服务及锁定依赖 |
| `scripts/`、`.github/workflows/` | 构建、测试、发布和安全门禁 |
| `docs/` | 当前文档与归档文档的版本、术语和实现状态 |

仓库当前约有 723 个 Go 文件、137 个 Python 文件、237 个 JS/Vue/TS 文件、209 个 Markdown 文件和 11 个 GitHub Actions workflow。数字来自 Git 跟踪文件，包含测试和文档，不等同于生产代码行数。

### 2.2 采用的判断标准

- “死代码”必须同时满足：生产入口不可达或无生产引用、没有迁移/兼容/插件反射用途、删除后不改变公共契约。
- “冗余逻辑”指同一业务事实在多个模块被解析、归并、排序或格式化，且实现可能产生差异。
- “过渡设计”指当前架构已经有明确的新权威模型，但旧模型仍被保留用于迁移、旧数据读取或旧客户端兼容。
- 测试、插件导出符号、反射注册、数据库迁移和历史数据读取不能仅凭文本搜索判定为死代码。

## 3. 当前 CI 错误及处理结果

### 3.1 Go Staticcheck

`node/internal/memory/store.go` 的 `memorySearchTerms` 先把字符串转换为 `[]rune` 再遍历，触发：

- `S1029`: 应直接 range string；
- `SA6003`: 不必要的 `[]rune(string)` 转换。

已改为直接对字符串进行 `range`。Go 的字符串 range 仍按 UTF-8 解码为 rune，不改变中文分词逻辑，同时去掉一次完整的 rune slice 分配。

### 3.2 Python pip-audit

`browser-service/requirements.lock` 中的 `pypdf==6.14.2` 被报告：

- `CVE-2026-84309`，修复版本 `6.16.0`；
- `CVE-2026-84310`，修复版本 `6.16.1`；
- `CVE-2026-84311`，修复版本 `6.16.1`。

不能只把锁文件改到 `6.16.1`：`browser-use==0.13.8` 的元数据精确要求 `pypdf==6.14.2`，会导致依赖解析失败。当前 CI 对 browser-service 的 `--no-deps` 审计明确忽略这三个 CVE，并保留原有的上游阻塞项忽略项；根依赖审计和后续新增告警仍会失败。

这属于临时门禁处置，不是安全修复。升级触发条件应为：`browser-use` 发布允许 `pypdf>=6.16.1` 的版本，或项目决定替换 browser-use。届时应删除这三个忽略项，并重新生成锁文件。

### 3.3 Go vulnerability scan 的额外发现

在本机默认 Go 1.26.5 上运行 `govulncheck` 时，`shared/update` 命中了 Go 标准库补丁版本相关告警（`net/url`、`crypto/tls`、`encoding/asn1`、`net/http`）。这不是本次远程 CI 已报告的失败项；使用 CI 固定的 Go 1.25.13 复跑 `shared/update` 后结果为 `No vulnerabilities found`。

因此当前不需要为了这次 PR 改动 Go 版本或忽略标准库漏洞。应继续保持 CI 与发布工具链的明确版本锁定，并在升级 Go 补丁版本时同步复跑漏洞扫描，避免本机默认工具链与 CI 的结果被误认为一致。

## 4. 代码清理结果

### 4.1 本轮已删除的内部兼容包装器与别名

以下符号已经确认没有生产引用，或只是在仓库内转发到当前名称；它们已从当前协议删除。内部 Go 符号不再保留额外发布窗口，真正需要保留的是旧数据库/旧历史数据的读取迁移，而不是旧函数名。

| 位置 | 旧符号 | 当前规范符号 | 建议 |
| --- | --- | --- | --- |
| `shared/config/config.go` | `DefaultFSRoot` | `DefaultRuntimeRoot` | ✅ 已删除 |
| `shared/config/config.go` | `Config.DataDir()` | `RuntimeRoot` / 明确的数据目录访问 | ✅ 已删除；Workgroup 独立的 `DataDir` 不受影响 |
| `node/internal/agentruntime/snapshot.go` | `EffectiveFSRoot` | `EffectiveWorkspaceRoot` | ✅ 本轮已删除包装器并删除仅为它保留的测试 |
| `node/internal/session/manager.go` | `SessionFSRoot` | `SessionWorkspaceRoot` | ✅ 本轮已删除包装器并更新替换测试 |

这些候选都不能与 `fs_root` 数据迁移混为一谈。删除的是 Go 名称，不是删除用户数据，也不是删除旧目录读取能力。

### 4.2 已完成的旧别名替换

`node/internal/history/journal.go` 中的 `JournalRelativePath` 已被 `RuntimeJournalRelativePath` 取代并删除；仓库内搜索没有残留生产引用。

### 4.3 本轮已完成的低风险收敛

在确认全仓生产引用后，本轮已完成以下清理：

- 删除 `queue.PriorityOther` 与仅被测试使用的 `ParsePriority`。当前控制队列只允许 continuation、resume 和 async completion 三类实际优先级；trigger 已明确走 `InputBox FIFO`，不再保留没有消费者的预留档位。
- 将压缩摘要的 workspace 历史路径调用统一到 `RuntimeJournalRelativePath`，并删除 `JournalRelativePath`。
- 删除 `node/internal/agentruntime.EffectiveFSRoot` 和 `session.Manager.SessionFSRoot` 两个内部旧名称包装器，测试统一使用 `EffectiveWorkspaceRoot` / `SessionWorkspaceRoot`。
- 删除 `ClassifyToolResult` 的等价转发函数，所有生产调用统一到 `tools.ClassifyResult`；前端仍保留状态解析，因为那是 UI 对 SSE 元数据的显示适配。
- 将 Session 恢复前的生命周期判断复用统一的 `loadLifecycleProjectionData`，删除 Manager 内重复的事件分页/Coordinator 重放实现；加载阶段得到的事件快照会直接传给 runtime，避免 Manager 创建/恢复 runtime 时再次读取同一批 SQLite 事件。直接构造 runtime 的测试/嵌入式调用仍保留无参数恢复兜底。
- 将 Session 创建判断所需的“记录是否存在”纳入恢复结果，避免空历史 session 为区分“新建/恢复”而再次查询 SQLite。
- 删除生命周期投影中的冗余 `loaded` 字段；投影是否存在由事件切片是否为空表达，读取错误由返回值表达。

这些删除没有改变队列顺序、历史格式、工具结果状态或 workspace 迁移行为。

### 4.4 当前确认不是死代码的部分

| 模块 | 原因 |
| --- | --- |
| `MessageQueue` | 现在主要承载控制、恢复和副作用通知；普通工具结果不走队列，但队列本身仍有生产引用，不能删除 |
| `RepairUnrespondedToolCalls` | 启动恢复、工具集变化和 Orchestrator 都会调用，是旧历史修复边界 |
| `RequeueInFlight` | 运行时错误和重启恢复仍依赖它 |
| `UnloadSkills` | API、Skills Manager、Turn 和子 Agent 权限策略仍在使用，不能因“Skills 元数据不再注入 system prompt”而删除 |
| `browser_run_task(wait=false)` | 保留：它仍是当前浏览器异步任务能力的真实入口，不属于已删除的 bash 后台协议 |
| `InputKindA2A` / `MessageSourceA2A` | 旧 A2A inbox 已移除，但当前子 Agent 和 relay 仍使用 A2A 来源标识 |
| `app/` | 旧 CLI 已移除，但 `app/config/env.py` 仍被 `run_dev_stack.py` 使用 |
| `docs/archive/` | 归档文档不是当前实现代码，不应作为死代码删除；它服务于迁移和历史追溯 |

## 5. 冗余逻辑与重构优先级

### P1：Workgroup 事件投影重复

Node Web UI 的 `useWorkgroupTimeline.js` 与 Manage Console 的 `WorkgroupChatView.vue` 都自行实现了：

- `parseAssignStartedText`；
- `parseNoticeTool`；
- assign 索引；
- activity/steps 投影；
- 工具类型和工具标签归一化。

两边已经出现差异：Node 有 `inferToolKind`，Manage Console 还保留自己的正则和审批辅助解析。只要事件文本稍有变化，就可能出现 Node 和 Manage 展示不一致。

推荐方案：后端输出稳定的、版本化的 Workgroup Timeline 投影事件，前端只负责渲染；短期若无法改协议，则提取一个无副作用的共享纯函数包，并为每种事件建立 fixtures。禁止两个前端继续复制正则解析。

### P1：MCP 双写入口

Node 同时提供：

- 结构化 `/v1/mcp/servers`；
- 文本编辑 `/v1/mcp/config`，内部通过 `ParseConfigText` / `FormatConfigText` 转换。

这不是死代码，因为设置页当前仍同时使用两者；但长期维护两个写入口会导致校验、命名冲突和错误提示不一致。

推荐将结构化 API 定为唯一写入权威，文本接口降级为导入/导出兼容入口，并标明 legacy；或者让文本接口内部最终调用结构化服务，不能保留两套独立的业务校验。

### P1：历史存储的权威边界不够清晰

当前 SQLite 的 `agent_runtimes.messages_json` 保存 hydrate 所需的完整消息快照，`turn_events` 保存 Turn/Step 生命周期事实，workspace 下的 JSONL 则保存原始消息审计侧车。三者各自有用途，但代码和注释曾把 SQLite 快照称为“compatibility snapshot”，容易让维护者误以为 JSONL 才是消息主库。

建议立即明确并写入架构契约：SQLite 消息快照是对话恢复与 hydrate 的唯一消息读取来源；`turn_events` 是生命周期的唯一事实来源；JSONL 只读审计，不参与恢复、排序或模型上下文。若未来要把历史主库迁移到 workspace，应设计一次性迁移，而不是继续让两套存储都可被业务读取。

### P1：Workgroup 双执行模型

`MemberCreateRequest.agent_id` 仍可为空，`WorkGroupMember.execution_mode` 仍在 `agent_ref` 与 `legacy_member` 间分支；Manage 侧对应保留 `member.provision`、本地 bridge 和 `agent.session.open` 两套路径。对当前已使用 AgentRef 的产品入口，这不是必要容错，而是完整的旧产品模型。

建议将 AgentRef 设为唯一成员模型：`agent_id`、`session_id`、`home_node_id` 必填，删除 `legacy_member`、`member.provision`、旧 bridge、对应的前端条件分支和测试夹具。该项会改变 Workgroup API，必须单独一个 breaking PR 完成，不能在普通 UI/CI PR 中零散删除。

### P2：Shell 双轨

`desktop/tray-tauri` 是推荐 Shell，`desktop/tray` 是 Go 兼容实现；发布和安装脚本仍同时覆盖 modern/legacy。这是有意的兼容设计，不是误留死代码，但它使 CI、发布包和问题定位成本翻倍。

只有在产品明确放弃旧 Windows/WebView2 兼容目标后，才能删除 Go Shell，并同步修改发布矩阵、安装器、升级代理和文档。

### P2：Update ownership 双轨

Node 的 `UpdateChecker`、`UpdateDelegatedToShell` 和 `/v1/agent/update` 仍共存。当前 Windows 更新检查已委托 Shell，Node 端点保留兼容状态。

建议保留端点但停止扩展其能力；在客户端全部迁移后，移除 Node 的周期检查、deprecated 字段和对应测试，并将升级责任固定在 Shell/Release Hub。

### P2：Workspace 命名过渡

当前生产路径已经统一到 `RuntimeRoot`、`WorkspaceRoot`、`WorkspaceStateRoot`；`fs_root`、`DefaultFSRoot`、`TurnOptions.FSRoot`、`WorkspaceModeLegacyShared` 等旧名称已从当前 Node 路径移除。剩余文档和旧快照读取代码仍需继续扫描，避免重新引入旧术语。

路径职责应保持清晰：

- `RuntimeRoot`：Node 运行时数据库、日志、锁和全局运行数据；
- `WorkspaceRoot`：Agent 工作目录、历史和记忆等用户可见/Agent 可操作内容；
- `WorkspaceStateRoot`：workspace 内部的 Agent 隔离状态；
- 旧路径字段：若仍出现在迁移代码中，只能作为一次性输入，不能进入新 system prompt、新 API 或运行时状态。

建议先统一新代码和提示词，再通过迁移指标确认旧调用方归零，最后删除旧名称。不要仅做全局字符串替换。

### P3：CSS 与 UI 样式重复

`node/webui/frontend/src/styles/workbench.css` 约 4902 行、超过 100KB，且与 `tokens.css`、`layout.css`、`overrides.css` 并存。已有审计统计出约 414 个颜色值、461 个字号声明、260 个圆角声明。

这不是功能死代码，但会制造视觉回归和尺寸抖动。推荐按页面/组件拆分，新增样式只能引用 design tokens；旧值按页面迁移，避免一次性重写造成大面积视觉回归。

## 5.1 进一步确认的过度设计点

以下问题已确认存在，但本轮不直接删除，因为它们要么仍有兼容责任，要么需要协议级改造：

1. **异步副作用协议仍比同步工具复杂**：bash 的 `run_in_background`、job registry/store 和通用后台回灌已经删除；当前保留的 `tool_callback` / `get_callback` 只服务 `browser_run_task(wait=false)` 等真正异步的浏览器任务。它不是死代码，但合并 callback、重新插入 tool 结果和继续 Turn 的路径仍然是高成本边界。建议后续把异步工具统一成“任务资源 + 状态查询 + 完成事件”三件套，避免继续为每一种异步工具定制历史消息。
2. **历史协议修复层次偏多**：当前同时存在 provider history 校验、legacy history sanitizer、turn 级中断修复和 lifecycle 恢复补偿。它们分别位于 LLM 边界、旧快照迁移、运行时取消和重启恢复；职责容易重叠，但直接合并会重新引入消息序列 400。推荐保留四层边界，统一由 `ValidateToolProtocol` 作为最终出口，并让其他层只产生修复计划，不重复判定合法性。
3. **结果状态有双层解析**：后端 `tools.ClassifyResult` 是状态权威，前端解析 SSE 已下发的结构化状态用于展示，消息正文仍承载模型可见的结果。两层目前有职责差异，但 `status`、事件字段、原始结果和展示阶段仍存在重复映射。建议建立一个稳定的结果 envelope，前端只读取 envelope，禁止再根据本地化正文推断业务状态。本轮已删除后端内部等价别名。
4. **生命周期恢复的直接构造兜底**：Manager 恢复链路已经携带事件快照，避免同一批事件的二次读取；只有绕过 Manager 直接构造 runtime 的嵌入式/测试调用仍让 runtime 自行读取。该兜底属于边界兼容，不应再扩展为第二套恢复实现；后续若所有调用方都统一经 Manager，可删除无参数分支。

因此，当前“过度设计”的主要表现不是无条件的死代码，而是**Workgroup 双执行模型、Node/Manage 双时间线投影、触发器字段别名、消息来源的结构化字段与旧 Name 双轨、异步完成消息的定制化回灌，以及 SQLite/JSONL 权威边界不清**。下一次清理应以协议/迁移窗口为单位，而不是继续做零散别名删除。

## 5.2 新发现的结构性收敛点

1. **Runtime 构造函数的状态参数过多**：`newRuntimeWithPublisher` 仍通过多组位置参数传入消息、Skills、Pending、Hook、通知游标、压缩标志等恢复状态。Manager 侧已先用 `sessionRestoreData` 命名承载，但到 runtime 边界仍被拆开，新增恢复字段容易出现错位传参。推荐下一阶段引入一个明确的 `RuntimeRestoreData`（或构造选项）对象，并让 `newRuntime` / `newRuntimeWithPublisher` 只接收该对象。
2. **冷 session 的只读接口重复重放事件**：`GetHydrateView`、`NotificationState`、`GetContextView` 等接口在 runtime 未装载时各自读取并重放 `turn_events`。这是正确性优先下的可接受实现，但高频刷新会重复 SQLite I/O。推荐以后增加带失效策略的只读 projection cache，失效依据使用最后事件序号；不要直接复用可变 Coordinator 实例，以免把读模型与运行时执行状态混在一起。
3. **触发器目标字段存在冗余别名**：`target_agent_id` 与 `target_session_id` 是两个合理的路由维度，但 `target_bound_agent_id` 与 `agent_target_mode` 又通过 `NormalizeCreateAliases` 映射到旧的 session 字段，导致前后端同时维护两套命名。建议保留 `target_agent_id`、`target_session_id` 和一个 `target_mode`，删除两组别名；如果需要更强语义，可改成一个结构化 `target` 对象。
4. **消息来源模型过度分层**：`MessageSourceKind`、`MessageSourceForm`、`MessageProvenance`、旧 `Message.Name` 四层共同描述一条注入消息。它解决了隐藏渲染、审计和旧历史识别，但大多数当前消息只使用 kind/form，provenance 仅少数路径需要。建议保留一个内部 `MessageOrigin{kind, operation, reference}`，在持久化边界一次性迁移旧 Name；不要让新业务继续依赖 `Effective*` 与 legacy source 推断。
5. **注册 payload 的身份字段重复**：`registerPayload` 同时发送 `node_id` 与标记 deprecated 的 `agent_id`，且两者值相同。若 Manage 当前已使用 `node_id`，应删除 `agent_id` 及对应客户端字段和测试，避免身份概念再次分叉。
6. **终端目标解析同时承担两种类型**：`resolveLinuxChannelID` 既接受模型可见的 terminal config ID，又接受内部 terminal session 解出的 channel ID。功能上必须支持两种对象，但当前用一个字符串函数承载两种类型，容易把“配置 ID”和“通道 ID”混淆。建议拆成 typed `TerminalTarget{SessionID, ChannelID}` 或两个明确的内部函数，保留模型层只传 `terminal_id`。
7. **记忆设置 API 仍沿用 `long_term_*` 命名**：当前实际存储已经是 workspace memory service，但 `prompt-context` 的 `long_term_scope`、`long_term_entries`、`global_long_term_entries` 仍把新旧实现混在同一组字段里。它们现在是工作中的 UI/API 契约，不是死代码；下一次前后端同步变更时应统一为 `memory_scope`、`memory_entries`、`global_memory_entries`，并删除旧命名映射。

## 6. 文档和版本漂移

当前 `VERSION` 和 `CHANGELOG.md` 已到 `0.10.7`，但以下现行文档仍写旧版本：

- `docs/README.md`：`v0.10.4`；
- `docs/architecture.md`：`v0.10.4`；
- `docs/development.md`：`v0.10.5`；
- `docs/design/agent-instance-model.md`：`v0.10.4`；
- 根目录 `AGENTS.md` 的 Workgroup 说明仍标为 `v0.10.4`。

这会让新贡献者误判当前能力边界，属于文档冗余和过渡信息，而不是代码死路径。建议：

1. 文档正文移除不必要的固定 minor version；
2. 必须展示版本时从单一版本源生成；
3. 归档报告保留历史版本，但现行入口只描述当前行为。

## 7. 推荐落地顺序

### 阶段 0：CI 和风险收口

- 合入本次 Staticcheck、Ruff、Pyright 和进程重启 E2E 修复；
- 保留并注明 pypdf 三项忽略的上游阻塞原因、负责人和复查触发条件；
- 升级 Go 补丁版本后复跑 `govulncheck`；
- 修正文档版本漂移；
- 清理审计报告中的已删除符号和过期测试基线。

### 阶段 1：消除真实重复实现

- 定义 Workgroup Timeline canonical event projection；
- Node 与 Manage 使用同一投影和 fixtures；
- 将 MCP 文本接口收敛为结构化 API 的导入/导出适配层；
- 明确 SQLite 消息快照、turn_events、JSONL 审计三者的唯一职责；
- 将 hydrate/context 的重复生命周期字段收敛到单一 `turn_state`。

### 阶段 2：结束兼容窗口

- 删除触发器 target/session 字段别名；
- 以 AgentRef 取代 Workgroup `legacy_member`（单独 breaking PR）；
- 在旧历史完成一次性迁移后，删除 Message.Name 的来源推断层；
- 在旧 Shell 支持策略结束后，删除 Go Shell 双轨。

### 阶段 3：结构性降复杂度

- 拆分 `runtime_lifecycle.go`、`runtime.go`、`coordinator.go` 和 `orchestrator.go` 的职责；
- 把“事件事实”“运行投影”“UI 视图模型”分别放入清晰的包/模块；
- 将 Workbench CSS 按 tokens、layout、component、page 分层，并为状态占位设置固定尺寸。

## 8. 验证基线

本次本地已完成或确认通过：

- Python unittest：194 个通过；
- Web UI Vitest：306 个通过；
- Node Web UI / Manage Console 构建和 lint 通过；
- 全模块 `go test ./...`、`go vet ./...`、Staticcheck 通过；
- Node session/turn/queue race test 通过；
- Ruff、Pyright、根依赖 pip-audit、browser-service 定向 pip-audit 通过；
- API/Workgroup contracts 检查通过；
- `git diff --check` 通过。

远程 PR 的旧失败状态不会因本地修改自动刷新；必须在获得提交/推送授权后重新运行 CI。审计期间不执行删除兼容代码、提交或推送操作。

## 9. 最终判断

当前项目的主要问题不是“留下了大量完全无用代码”，而是“核心路径已经收敛，但几个完整的旧模型仍与当前模型并行存在”。最值得优先投入的是 Workgroup 双执行模型、Node/Manage 投影重复、触发器目标字段别名、历史存储权威边界和结果 envelope；最不应该做的是凭名称删除 A2A、Placement、Skills unload、MessageQueue、浏览器异步任务或旧 Shell 的全部符号。

后续每次清理 PR 都应附带三项证据：生产引用搜索、旧数据/插件兼容判断、删除后的测试与迁移验证。这样才能把一次性补丁逐步收敛成可维护的架构，而不是制造下一轮兼容分支。
