# DAgents 代码清理与开源工程化整改清单

> 审阅日期：2026-08-24
> 审阅分支：`codex/sync-v0.9.18-dev`
> 范围：Node、Manage、Web UI、Client、桌面 Shell、共享模块；本轮不调整 MCP/终端两个面板的布局与交互。

## 1. 结论摘要

当前项目已经具备可运行的多模块架构，但仍处于“快速演进期”：核心运行链路已经有较清晰的 `session → turn → step → event` 方向，外围仍存在兼容层、双轨实现、历史工具名和未统一的工程门禁。

本轮只清理了有静态证据证明无调用关系的代码，未删除仍承担历史快照、旧客户端或低版本 Windows 兼容职责的实现。清理后核心 Go 包测试通过，定向 Staticcheck 通过。建议下一阶段优先做协议和生命周期抽象，而不是继续按文件逐个打补丁。

## 2. 本轮已完成的清理

### 2.1 已确认无引用的代码

删除以下无调用符号/字段：

- `api.companionMetaJSON`
- `browser.errorResult`
- `fakePathStater.set` 测试辅助方法
- `stubPhaseHook.priority` 测试字段
- `runtime.applyStepOutcome`
- `runtime.setLoadedSkillsByName`
- `runtime.unloadSkillsByName`
- `store.stripLongTermEntryPrefix`
- `triggers.maxFloat`
- `turn.stripLongTermEntryPrefix`

这些删除均来自 Staticcheck `U1000`，不是基于“看起来没用”的主观判断。

### 2.2 已确认的冗余/低风险问题

- 删除 `baseDefaults`、Linux 远程命令包装器的无效初始赋值。
- 删除 `resumeDiagSnapshot` 和 `finishTurnIdle` 中的空临界区。
- 将副作用消息逐字段复制改为同构类型转换。
- 将消息追加循环改为 `append(..., plan.Messages...)`。
- 将可取消调用点从 nil Context 改为明确的 `context.Background()`。
- 统一几处错误字符串首字母格式，避免错误包装和日志消费出现不一致。

## 3. 明确不删除的兼容代码

以下代码目前看起来像“重复实现”，但有明确兼容语义，本轮保留：

| 区域 | 当前状态 | 为什么不能直接删 | 建议 |
|---|---|---|---|
| `linux_exec`、`linux_file_upload/download` | 新 Agent 默认快照已不再暴露，旧快照仍可解析 | 旧 Agent 快照、旧历史消息和旧客户端仍可能依赖旧名称 | 保留一个版本窗口；加调用遥测和迁移提示，窗口结束后单独删除 |
| `desktop/tray` 与 `desktop/tray-tauri` | Go 兼容轨与 Tauri 推荐轨并存 | 面向低版本 Windows 的系统浏览器路径仍有产品价值 | 抽出共享 Desktop API/协议；明确支持矩阵和退出日期 |
| `RenderLoadedSection` | skills 旧正文渲染接口仍有诊断/测试用途 | 直接删除会扩大 skill 上下文迁移范围 | 标记为 diagnostics-only，禁止新运行链路依赖 |
| Manage `legacy_member` | 工作组模型仍保留旧成员模式 | 注册表查找与 agent 引用迁移尚未完全闭环 | 完成 Node registry lookup 后再收缩模型联合类型 |
| YAML/旧配置迁移字段 | 仍承担升级和回滚 | 删除会破坏已有用户运行目录 | 迁移成功率达到阈值后再分版本移除 |

## 4. 主要结构性问题

### P0：协议和状态源仍需进一步收敛

1. Node 内同时存在运行时内存、生命周期投影、队列、SSE/WS 展示状态和持久化快照。必须明确：事件日志/TurnCoordinator 是权威事实，UI 状态只能由事件投影产生。
2. Manage 与 Node 的工作组通信已经选择 Node 主动建立 WS，但协议仍需具备版本、能力、序列、确认、重连和幂等语义。
3. 多个入口仍可以表达类似的“执行命令、工具结果、异步回调”，需要统一 envelope 和终态模型。

### P1：边界接口不够稳定

1. 工具实现、审批、HITL、异步回灌、终端会话之间仍有隐式约定，部分字段靠字符串约定。
2. `channel_id`、`terminal_id`、`session_id`、`agent_id` 的职责不同，但部分旧接口仍容易混用。
3. Python、Go、Vue 各自维护 DTO，缺少从单一契约生成或至少自动校验的机制。

### P1：工程门禁不完整

当前 CI 覆盖 Go 测试、Web UI Vitest、Python unittest 和构建，但没有统一的 Staticcheck、前端 lint/dead-code 检查、API 契约测试、迁移测试矩阵。审阅时发现 `shared/config` 的模块声明为 Go 1.22，而测试使用了 Go 1.24 才提供的 `testing.Chdir`；本轮已改为兼容 Go 1.22 的临时目录切换，后续仍需统一各模块的工具链门禁。

### P2：兼容层缺少系统化治理

兼容逻辑分散在工具注册、快照迁移、Manage 模型、桌面 Shell 和前端展示中。现在能工作，但很难回答“哪一版开始不再接受某个字段/工具名”。需要统一 deprecation policy、遥测、文档和删除版本。

## 5. 开源项目工程化整改清单

### A. 统一领域模型和接口

- [ ] 建立 `ExecutionTarget`：`local_shell`、`linux_channel`、`terminal_session`、`browser` 等目标统一描述，但不把不同能力强行合成一个工具。
- [ ] 建立 `ToolExecutor` 接口：输入为版本化 `ToolExecutionRequest`，输出为结构化 `ToolExecutionResult`，统一 `status`、`error_code`、`target`、`duration`、`truncated`、`artifacts`。
- [ ] 建立 `TerminalSessionBroker`：负责 open/input/resize/subscribe/close、会话所有权、并发和空闲回收；SSH、PTY、Windows ConPTY 仅实现 provider。
- [ ] 为工具、终端、工作组 WS、SSE 使用统一事件 envelope：`event_id`、`source`、`subject`、`session_id`、`turn_id`、`step_id`、`seq`、`occurred_at`、`payload_version`。
- [ ] 所有跨层函数禁止 nil Context；接口文档明确取消、超时、重试和幂等语义。

### B. 收敛 Turn/Step/队列生命周期

- [ ] 保持 `TurnCoordinator` 作为唯一 Turn/Step 身份源，删除或隔离旧的镜像状态字段。
- [ ] 把“事实事件”和“派生状态”分开：工具执行完成、模型开始生成、模型完成、取消、压缩、终端断开都是事实；`running`、`thinking`、`waiting_tool` 是投影。
- [ ] 队列 envelope 只承载可序列化、可恢复的身份，不通过隐式闭包传递生命周期。
- [ ] 为取消、迟到回调、重连恢复、异步工具结果增加表格化状态转移测试。
- [ ] 为每个 Step 增加可观测的开始/结束原因，避免只依赖 `phase` 或 `terminal` 推断结果。

### C. 工具注册与描述标准化

- [ ] 工具定义集中维护名称、版本、参数 Schema、返回 Schema、风险等级、目标能力和兼容别名。
- [ ] API tools 数组是模型可见定义的唯一来源；system prompt 只解释选择原则和行为约束，不重复完整 Schema。
- [ ] 参数 Schema 对必须字段使用 `required`，对互斥参数使用 `oneOf`/约束校验，并在 Node 端二次校验。
- [ ] 兼容工具名只在旧快照解析层映射，禁止在新注册表和新系统提示词中重新暴露。
- [ ] 工具返回统一区分模型上下文、用户展示、审计详情，避免同一大段输出在三层重复注入。

### D. ContextInjection、附件和 UI 目标上下文

- [ ] 上传附件、当前聚焦页面/终端、人工选中的终端、当前工作组等都使用结构化 `ContextInjection`，携带 `source`、`provenance`、`scope`、`created_at` 和稳定 ID。
- [ ] 明确 injection 的作用域：request-only、turn-scoped、session-scoped；禁止把 UI 瞬时状态永久写入历史。
- [ ] 将附件元数据与内容引用分离，模型只在需要时读取内容，避免大对象破坏上下文缓存。
- [ ] 对“当前页面/当前终端”使用显式 pinned target，而不是每次请求从前端模糊推断。

### E. Node/Manage WS 协议

- [ ] Node 发起连接；Manage 不新增反向 HTTP 调用。
- [ ] 握手包含 `protocol_version`、`node_id`、`agent_catalog_revision`、`capabilities`、认证信息和客户端时间。
- [ ] 消息包含单调 `seq`、`ack`、幂等 `request_id`；断线后按 seq 恢复或明确 full-resync。
- [ ] Node 只接受属于本节点、agent、session 的命令；Manage 下发工作组任务时携带 `workgroup_id`、`member_id`、`conversation_id`、`turn_id`。
- [ ] 明确 agent 多会话隔离：持久化主键至少包含 `agent_id + conversation_id/session_id`；UI 和工具回灌不得只依赖 agent_id。
- [ ] 连接、重连、心跳、鉴权失败、版本不兼容和节点下线都形成可观测事件。

### F. 持久化与安全

- [ ] 为 SQLite 表引入显式 schema migration version，不在业务启动路径隐式猜测迁移状态。
- [ ] secrets/credential 只保存引用或加密材料；日志、事件和错误消息禁止输出密码、私钥和完整 token。
- [ ] 对 agent 快照、工具清单、skill revision、工作组成员 spec 建立 digest 和 revision 约束，避免并发覆盖。
- [ ] Repository 层统一返回领域错误码；HTTP、WS、SSE 层只负责映射，不自行拼接业务错误字符串。

### G. Web UI

- [ ] 建立 typed API client 和事件 reducer，页面状态不再通过多个 watch/heuristic 互相推断。
- [ ] 所有 loading、thinking、tool-running、waiting-approval、completed、failed、cancelled 状态来自权威事件；展示可以合并，但不能改变事实语义。
- [ ] 为消息页、终端工作台、MCP 面板设定稳定的组件边界和 z-index/overlay 约定；本轮不改现有两个面板布局。
- [ ] 为 Node Web UI 与 Manage Console 增加 lint、格式化、组件测试和关键流程的浏览器回归测试。
- [ ] 对 terminal input/output、附件注入、切换 agent/session、断线恢复补充可重复的 UI 测试清单。

### H. CI、发布和贡献者体验

- [ ] CI 增加 `go vet`、Staticcheck（至少 U1000/SA/S10/ST10 选定规则）、Python lint/type check、前端 lint/dead-code。
- [ ] 增加 API schema compatibility、WS replay/idempotency、SQLite migration 和旧快照升级测试。
- [ ] 增加最小 E2E：创建 agent → 发消息 → 工具调用 → 异步回调 → 取消 → 重连恢复。
- [ ] 建立版本化 deprecation policy：宣布版本、遥测期、警告期、默认关闭期、删除版本。
- [ ] 增加 ADR、贡献指南、CODEOWNERS、PR 模板和安全问题报告流程。

构建审阅还发现两项非阻断警告：Node Web UI 主 JS bundle 约 1.2 MB，后续应按路由/重量级依赖做 code splitting；Manage Console 的 `page-agent` 第三方依赖包含 `eval`，应在依赖升级或安全审查中单独处理，不能简单通过压制构建警告解决。

## 6. 建议落地顺序

### 第一阶段：低风险门禁与契约（P0/P1）

1. 固定 Go/Python/Node 工具链版本，修正 `shared/config` 的 Go 版本不一致。
2. 将本轮静态检查纳入 CI，并补全 API/WS envelope 的契约测试。
3. 冻结 `ToolExecutionResult`、事件 envelope、错误码和 ID 语义。
4. 为旧工具名和旧成员模式加遥测与 deprecation 标记，不立即删除。

### 第二阶段：运行时与通信抽象

1. 完成 `TerminalSessionBroker` 和 `ToolExecutor` 的 provider 化。
2. 让 Turn/Step/queue/event 使用同一套身份和恢复协议。
3. 完成 Node 主动 WS 的 ack、重连、幂等、full-resync。
4. 完成 agent 多会话隔离和工作组成员 agent registry lookup。

### 第三阶段：前端与兼容层收敛

1. UI 全部迁移到事件 reducer 和 typed client。
2. 旧工具/旧字段经过至少一个稳定版本的遥测确认后删除。
3. 评估 Go Shell 兼容轨的保留期限，避免双轨无限期维护。

## 7. 验收标准

- 新增或修改的运行时状态都能追溯到一个权威事件，UI 不依赖模糊的时间窗或局部副作用。
- 同一个 agent 的两个 conversation 不会共享消息、工具结果、终端输入输出或取消信号。
- 断线重连不会重复执行不可重试工具；重复消息可以通过 request/seq 幂等处理。
- 旧快照继续可读，兼容路径有遥测，并且文档写明删除版本。
- CI 能在干净环境完成 Web UI、Go、Python、桌面相关构建和核心 E2E。
- 新贡献者可以通过文档理解模块边界、事件协议、错误码、迁移和测试入口。

## 8. 本轮验证记录

已执行：

- Node 核心包：`go test ./internal/api ./internal/browser ./internal/compression ./internal/hooks ./internal/session ./internal/store ./internal/tools ./internal/triggers ./internal/turn`
- 定向 Staticcheck：`U1000, SA4006, SA1012, SA2001, S1008, S1011, S1016, ST1005`
- `git diff --check`

结果：上述核心包测试全部通过，定向 Staticcheck 无剩余问题，差异检查通过。工作区已有的 `.codex/` 和 `dagents-node.exe` 未触碰。

审阅期间发现的 `shared/config` 与 Go 1.22 声明不匹配问题已修复：测试不再依赖 Go 1.24 的 `testing.Chdir`，而使用可恢复的 `os.Chdir`。修复后 `go vet ./node/... ./client/... ./shared/config/...` 及其他共享模块、桌面兼容轨的 vet 均通过。
