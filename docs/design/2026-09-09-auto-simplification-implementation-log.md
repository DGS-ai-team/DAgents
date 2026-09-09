# Auto 简化实施历史记录

本文件保留各批次当时的结果，早期未完成项或判断可能已被后续批次修正；不作为当前开发要求。当前方案和状态以 [实施方案](2026-09-09-auto-simplification-plan.md) 为准。

## 7. 实施状态

2026-09-09：新方案已确立，旧方案退役。工作区起点 `62743538`。已安排 Luna 审计旧链路删除范围与 UI 依赖，并开发独立配置/Todo/经验存储基础。以上是开发目标，尚未宣称运行行为已切换。

实现审查提醒：`session.Manager.ClearContext` 当前调用 `clearMessages`/`Store.ClearMessages`，不得直接作为 dreaming 的“保留历史、重置活跃上下文”实现。需区分历史展示与模型输入起点，并验证重启后的读取。

首批基础完成：`node/internal/autonomy` 独立保存配置、逐项 CAS Todo（含删除）与单个当前经验字段（dreaming 专用 CAS 提交方法）；主 Agent 全包 race 复验通过（1.420 秒）。此包尚未接入生产运行时，经验写入权限仍需运行时落实。侧栏底部重复自主设置按钮已移除，三组和总览保留，主 Agent NavRail 2 项测试通过。尚未完成默认 trigger、主会话/system prompt、dreaming、新 UI/Manage 和旧路径删除；不得将基础包通过视为完整改造完成。

### 替换依赖核对

- 旧 Auto API 集中在 `autonomy_api/actions/cycles`、`auto_cycle_controller`、`auto_intent_projector`；旧 `goal_runtime` 创建专属 Session。新激活不得调用这些路径。普通 goals API 与其生命周期共享代码需独立保留测试。
- 独立事件源涉及 `node/internal/events`、`event_sources_api`、工具 `event_source_list`、scheduler event poller；须连同生产注入点一起移除。
- 现有 `triggers.Store` 更新和 `Scheduler` fire 都明确拒绝 `condition.cmd`，因此脚本条件需要恢复并验证，不能直接宣称现有执行可复用。使用 Agent 归属的执行环境，明确超时、失败及条件不满足时的下一次检查；不可绕过原有授权规则。
- `TurnBudget.MaxToolCalls` 计数单个工具，不能直接映射“工具轮次”。需核对 `MaxSteps` 和最终收尾的语义，实测一批多工具只消耗一轮、达到上限后无新增工具执行。
- 普通 trigger 已有持久化 pending delivery 与 runtime 消费前身份检查，可复用为默认唤醒合并及关闭后失效。`InputBox.Pop` 当前是严格 FIFO，并不保证后到用户消息先于默认唤醒；须增加默认激活专属优先级处理，不能把“串行”当作“用户优先”。
- 下一批 API 使用新配置入口，开发期间不得在旧 UI 上伪装频率已生效。默认 trigger、同主会话及工具上限接通后才切换正式设置入口，最终删除旧入口而不是保留兼容层。

### 职责与配置接线批次（基础验收通过）

新增 `/auto-config`、`/todos`、只读 `/experience` API；职责和经验通过 Agent 归属 provider 进入实际模型请求的 system prompt，每个 Turn 冻结，下一轮（包括 trigger 输入）更新。provider 读取失败不调用模型。独立 `SimplifiedAutoPanel` 按新接口构建，保存与加载按 Agent 隔离、经验只读；尚未挂到正式设置入口。

仍需注意：无活跃模型快照时，`SystemPromptForSession` 的上下文预览与压缩前缀暂未纳入新 provider；不能将模型请求已注入等同于全部诊断界面已对齐。Todo 暂只有持久化/HTTP 操作，模型工具及激活前读取尚未接入。当前新接口的激活频率与 dreaming 为配置存储，未驱动执行。旧 Auto 生产链仍待下一批替换移除，不是兼容承诺。

主 Agent 独立验收：Session 全包 race 29.127 秒、Turn 全包 race 10.521 秒；API race 排除已有 Windows 截屏项和当时未完成的新主会话接缝项后通过（73.547 秒），随后新 Auto API 三项含该接缝 race 通过（1.700 秒）。接缝经真实 HTTP PUT→注册 Agent 主会话 `/v1/messages`→本地 httptest 模型服务，捕获请求 system 内容验证首次职责与下一轮修改，未调用真实提供商。新面板/侧栏共 6 项测试通过；OpenAPI YAML 解析及 86 个内部引用解析通过。完整路由合同仍有既有 policy grants 三个缺失路径，未将其宣称通过。未进行新版浏览器视觉验收。

### 默认唤醒存储底座

新增 `triggers.Store.EnsureAutoDefault`，每 Agent 稳定 `auto-default:<agentID>`，绑定 `controller=auto` 与同 Agent 主 Session；使用方案固定唤醒语句。相同配置不重排，修改间隔更新下次时间；关闭保留合法禁用定义，在保存成功后失效 pending delivery。拒绝身份碰撞、普通及授权路径的编辑删除。保存失败保留内存与磁盘状态，不重置触发历史。

主 Agent Triggers 全包 race 通过（2.040 秒）；Session/Turn 当前共享树全包 race 通过（28.551 / 10.731 秒）。默认唤醒底座尚未接入配置同步或启动校正，因此未实际启用。重启 pending 沿用现有 recovery 语义，接线时仍需完成恢复；用户优先、单次工具轮次、脚本条件和旧链路删除仍未完成。

### Todo 模型工具与请求上下文

模型工具 `todo_list/create/update/delete` 绑定可信 Agent 身份与 HTTP 同一存储，逐项版本校验，不能通过参数指定其他 Agent；普通 runtime 不提供这些工具，旧 Goal runtime 的禁用标记继续有效。工具复用原有审批，未新增自动放行规则。字段解析区分省略和错误类型，更新状态不得覆盖正文。

每个新 Turn 将最新 Todo（含 ID、状态、revision，空列表亦明确说明）注入请求级上下文，不写入 system prompt 或会话历史。职责与经验仍在 system prompt；空闲预览读取最新 provider，活动 Turn 保持冻结。较早记录的 idle 预览缺口已在本批处理。

主 Agent 真实 HTTP 主会话→本地测试模型→Todo 工具→共享存储→后续模型请求接缝通过；临时测试策略明确允许该工具，未改生产审批。Auto API / Todo 工具专项 race 通过（2.186 / 2.132 秒），shared/config 通过（0.578 秒）。执行编码的一个 Luna 子任务曾遇额度中断，另一 Luna 已接手补齐接缝测试。默认定时激活、单次轮次限制、用户优先及 dreaming 仍未接通，不将主会话工具成功当作整个自主循环完成。

### Todo 前端独立面板

新增可折叠 `AutoTodoPanel`，支持文本和状态编辑、创建与版本校验删除；切换 Agent 清理草稿和保存锁，旧请求不能影响新 Agent。加载失败禁止写入并提供重试；同 Agent 冲突重载保留草稿，服务器已删除条目时可恢复到新增输入框。

修复新配置和 Todo 的前端请求封装：原实现把 method/body 放到未被读取的第三个参数，删除版本字段亦不匹配；现按实际 fetch 封装传入方法及 expected_revision。组件 mock 测试不足以发现此类问题，增加请求层断言。主 Agent 复验 Todo 面板、新配置面板及请求测试共 12 项通过。面板尚未挂入正式聊天入口，也未完成新版视觉验收。

### 默认激活接线与轮次限制

配置 PUT 现同步默认 trigger；相同频率不重排，关闭禁用。Profile 为配置事实，trigger 为确定性派生：两份文件不宣称原子事务。触发器写入失败返回 503 和已保存 Profile（含新版本），可调用 `POST /auto-config/reconcile` 只修复派生记录，不再次递增 Profile；配置保存与修复串行。正式 UI 接入时需呈现这一部分成功状态并提供重试。

启动按未归档 Auto Agent 的新配置重建默认 trigger，缺配置默认关闭，普通 Agent/trigger 保持原行为。未决 delivery 重启后仍按既有 recovery 机制保留并阻止调度启动，不自动重放；这项恢复体验尚需在最终切换前收尾。

主会话每次默认激活校验 Agent 类型/归属、稳定 trigger ID、固定目标会话和 pending delivery，读取当前频率与工具轮次。配置关闭、触发器未同步、归属错误或 provider 失败时不调用模型。可信激活采用仅工具轮次和一次无工具收尾的预算，普通聊天/普通 trigger 保持原有预算。职责、经验及 Todo 沿用前批同一主会话注入链路。

主 Agent 复验 API 新配置/启动专项 race 3.196 秒，Session/Turn 全包 race 32.708 / 11.077 秒，Triggers 全包 race 1.851 秒。测试模型为本地桩；真实提供商与视觉验收尚未进行。旧 cycle/intent/event-source/maintenance 生产入口仍待删除，不能把新默认激活已接通表述为全部架构切换完成。后续依次处理旧入口删除、用户输入优先、脚本条件、dreaming、正式 UI/Manage 及真实验收，不新增旧消息兼容层。

### 旧 Goal 保留范围的证据修正

清理时重新核查 `goals_api.go:createGoal`：无论 managed 参数取值，均要求 Agent 类型为 auto，并创建 `goal-session-*` 专用会话及 `CreatedBy=autonomy` 的 Goal trigger。因此此前“保留普通 Goals API”的判断不成立，这套 API 也是旧 Auto 产品入口，应与旧 Auto cycles 一并退役，不能为了保持旧测试绿色而继续提供独立自主会话。底层存储类型若仍被通用代码引用，按实际依赖继续拆除；通用聊天、用户 trigger、审批与文件手册不受此结论影响。

### 用户输入优先验收

系统默认唤醒通过 scheduler 专用入队路径设置内部 `system_auto` 类型，要求 trigger/delivery 身份。空闲取数时，用户可越过队首连续默认唤醒；普通 trigger 和 child 输入作为屏障，保持原相对顺序。不会打断正在执行的模型轮次或另开循环。恢复路径及关闭后的 delivery 失效检查覆盖新输入类型。

真实 session 测试阻塞首轮模型，在忙碌期间依次入队 Auto 与用户，解除后捕获实际请求证明用户先执行；失效的 Auto delivery 不调用模型。主 Agent Session/Triggers 全包 race 通过（33.661 / 1.838 秒）。

### 正式前端入口切换

智能体设置切换为新配置面板；保留自定义激活频率、只读经验、草稿与已保存版本分离、Agent 切换隔离。503 部分保存按真实 `error.details.saved_profile` 更新已保存版本，保留编辑草稿并提供只同步 trigger 的重试。dreaming 尚未执行，界面暂只读并明确开发中，不覆盖已保存值。

主聊天嵌入默认折叠 Todo；移除旧 Goal 专用会话分流、Goals/AutoWork 路由与对应页面，以及旧事件源/维护设置挂载。侧栏仍保留 Auto 总览入口；总览使用新状态、next_at、todo_counts、todo_summary，并保留搜索、筛选与分页。主 Agent 全前端测试通过（69 文件、394 项），Luna 构建通过。此处是代码入口与测试验收，浏览器仍连接旧隔离 Node 18766，尚未完成新版运行环境的真实交互或视觉验收。

### 旧后端退役与 Node / Manage 总览切换

撤除旧 Goal/Cycle/intent/event-source/maintenance 的 HTTP 入口、工具注册与启动调度，不再打开旧 goals/events 存储，也不为旧消息的专用 session 参数提供兼容。保留通用聊天、用户 trigger、审批及手册文件。OpenAPI 同步删除退役接口及专属模型，47 个内部引用可解析。仍有未接线的旧底层类型及前端组件，后续按依赖收尾，不能据此宣称全部旧源码已清空。

Node 与 Manage 共用默认激活状态投影，使用 working / standby / activation_off / needs_attention；无效、禁用或待恢复默认 trigger 明确呈现异常，不伪造下次执行时间。Manage 只接收状态、频率与 Todo 数量，不上报职责、Todo 正文、经验或工作目录；控制台保留上报时间与过期标记。

主 Agent 验证：API / Manage reporter / Agent runtime 全包 race 通过（26.592 / 6.793 / 3.236 秒）；工具包普通测试通过（13.530 秒）。工具包 race 在 Windows 第三方 screenshot 的真实显示器枚举发生 checkptr 崩溃，不能宣称其完整 race 通过。Python Manage 8 项通过，Manage Console lint 通过；Node 前端本批此前 69 文件、394 项通过。

剩余开发：trigger 条件脚本、默认 delivery 重启恢复体验、每日 dreaming 的经验写入与上下文边界、残余旧代码清理、最新运行环境下真实 LLM 与 Node / Manage 视觉验收。dreaming 当前仍是禁用的开发中 UI，整个目标保持进行中。

### Dreaming 与条件脚本的实施约束（进行中）

Dreaming 使用固定的上下文边界 token：在持有同 Agent 执行租约且 dreaming 已完成时采集，重试不得重新采集。经验提交与恢复标识在 autonomy 存储中原子写入；随后 session 持久化历史归档与活跃上下文重置，再确认 reset_applied。上一笔提交尚未完成重置时，不得提交次日经验。普通会话历史查询合并归档与活跃消息，模型请求、上下文预览和压缩只使用活跃部分。提交记录只需经验摘要 hash，不按天复制整篇经验。

验收必须覆盖：经验保存失败保持旧状态；经验保存后重启能找到未完成重置；重置后确认失败可以幂等重试；重试不清除后续用户消息；模型下一轮及重启后均不再携带旧正文，历史界面仍能查看。底层接口不等同于每日调度和真实 dreaming 已完成。

条件脚本以当前 Agent 的工具执行环境与原有 policy 为准，不在 scheduler 直接调用不受约束的宿主 shell。执行脚本之前必须完成归属、revision 与当次执行资格校验。重复调度/手动并发不能重复执行同一次脚本；false 或失败记录结果并按规则推进检查时间，持久化失败不能被忽略。条件满足才投递主会话。缺少执行器时明确失败，不能把跳过条件当作满足。

Dreaming 原子提交存储首批已验收：经验正文与含内容 hash 的提交标识同次保存，按 Agent/日期去重，上一条未确认重置时拒绝下一条，提供 pending 查询与幂等确认。测试覆盖并发同日只成功一次、失败回滚、确认失败仍可恢复、跨 Agent、重开读取和非法持久化数据。主 Agent autonomy 全包 race 通过（1.475 秒）。这只证明持久化底座，session 边界与每日执行器尚待验收。

条件脚本调度底座已验收：runner 调用前持久化 claim，校验定义 revision 和 occurrence；false/error/未配置执行器不投递，推进定时检查并释放 claim，释放保存失败明确报错且保留 pending。专项覆盖并发调用、旧 revision、跨 Agent、旧 occurrence、false/error/nil 及清理失败；主 Agent Triggers 全包 race 通过（1.737 秒）。生产尚未注入经过 Agent 工具与审批的 runner，API/模型参数入口仍待同步，不能将此底座表述为脚本唤醒功能已上线。

脚本生产接线审计：真实工具为 bash_run（tools/bash_run_tool.go、bash_runner.go），空 cwd 使用 Agent workspace。Registry.Execute 只分发工具，不代表已审批；应在所属 Agent 的 session/turn 中复用 tool_router 的 hooks/policy/preflight，并处理现有 HITL。当前 ConditionRunner(bool,error) 对任何 error 都释放 pending，不能直接承载待审批的持久化恢复；接线时必须显式区分待审批与执行失败，审批前不得执行脚本，审批恢复须重新验证身份与配置。该执行入口、超时取消和 API/模型 cmd 参数尚未完成。

### 活跃上下文边界与 Dreaming 执行器（底层接缝验收通过）

新增 CaptureActiveContextBoundary / ResetActiveContext，要求当前 Agent 执行租约；token 固定 session、revision、索引和前缀摘要。重置只移动活跃起点，不删除完整历史，拒绝边界回退或正文变化；持久化失败回滚。生命周期工具恢复、模型请求、上下文预览和压缩均处理活跃尾段，写回时保留归档前缀。显式清空聊天仍清除历史，并同步清理边界状态。

新增 RunDreaming，在同一 session 和执行租约中运行模型及手册文件工具，沿用原审批。使用 SideEffect 来源和独立工具轮次上限，允许一次无工具收尾，不复用 trigger 字段或 token 预算；结束后恢复聊天预算。仅正常完成、无待审批/错误且存在最终无工具文本时返回可提交经验；未知 token 用量不阻断。不在该方法内写经验或重置上下文，供每日执行器按已设计提交顺序调用。

新增接缝测试通过真实 Manager、SQLite 和文件工具执行三轮，其中包含两次重置和一次 Stop/重开。捕获本地测试模型请求验证无旧正文、工具协议有效；实际 GetHydrateView 输出验证各轮历史不重不漏；ContextView / ContextSummary 只显示活跃部分；显式清空后聊天继续。另验证工具上限为 1 的无工具收尾、审批阻断文件写入、取消、未知用量及原预算恢复。

主 Agent 最终复验：Session / Store 全包 race 通过（43.338 / 6.200 秒），API / Agent runtime 全包普通测试通过（26.626 / 2.485 秒）。本地测试模型不等于真实提供商验收。每日调度、经验提交与上下文重置的上层串联、启动 pending 恢复、前端 dreaming 启用与状态、脚本生产审批执行器、残余清理及真实 UI / LLM 验收仍未完成，目标继续进行。

### 每日 Dreaming 接线与设置页（本批）

每日调度接入 Node 生命周期，独立于自主激活频率；使用 Agent 主会话及执行租约，同日去重，忙碌延后，失败固定五分钟后重试。串联 RunDreaming、经验原子提交、固定边界上下文重置及确认；启动后周期检查未完成提交，关闭 dreaming 仍可完成已提交记录的重置恢复。提示词明确整理已有经验、近期上下文与 Todo，长内容写文件手册并保留相对路径索引。

新增只读 dreaming 状态 API 和设置页状态、时间、错误及刷新；每日开关、整理时间、时区分别一行。保存后刷新状态而不覆盖草稿，切换 Agent 隔离旧请求，经验读取失败不伪装为空。OpenAPI 同步，49 个内部引用有效。

主 Agent 验证：前端 64 文件、350 项测试通过；每日调度专项 race 2.570 秒通过，覆盖关闭自主激活仍整理、初次并发只运行一次、关闭 dreaming 后恢复 pending、重开存储和 Manager 后恢复不调用模型、失败退避后重试。普通/Auto/不存在 Agent 的状态接口分别断言 409/200/404。API 全包首次运行失败，输出未保留完整诊断；随后保留 JSON 日志的全包重跑通过（22.435 秒），首次失败原因仍未定位，不能宣称已经修复。真实提供商与浏览器视觉验收尚未完成。

另外已删除残留的旧事件源、维护及风险观察前端组件和入口。条件脚本仅完成 policy 执行底座；可持久恢复的既有 HITL 接线仍在开发，不能宣称生产脚本触发已可用。默认 trigger 重启恢复体验、dreaming 总览/Manage 投影、残余后端清理仍为缺口。

### Node / Manage Dreaming 总览

总览增加独立 dreaming 状态及可选下次/最近成功时间，激活状态和筛选仍保持原义。调度器不可用显示 unknown，不伪报关闭；Manage 接收严格字段模型，校验带时区时间，仅保存状态和时间，不接收经验、手册路径或错误详情。Node 表格与 Manage 桌面/窄屏布局同步展示，dreaming 摘要放在状态标签之外，避免形成大型标签。

主 Agent 复验：Node 前端 64 文件、351 项测试通过，Manage lint 通过；API / Manage 总览专项 race 1.375 / 1.077 秒通过；Python Manage 全模块 9 项通过，包括实际 HTTP 写入/读取、SQLite 重开后 dreaming 保留和私有字段拒绝。Luna 完成两端构建。OpenAPI 修正已退役风险观察接口和旧 Manage 字段，Node/Manage 内部引用分别 50/6 个有效。

浏览器现场确认 Vite 5173 仍连接旧后端 18766，页面提示 Auto 配置不可用；本批尚无新版视觉或真实模型验收结论。脚本条件的审批恢复正在审查，重点是执行前持久化 started、非法回复不消费审批、恢复不重放以及不清空既有历史。当前尚无 session 条件端到端测试证据，不能把工具层单测当作会话接线完成。

### 退役后端配置清理与重复回归

删除无人调用的 WithMaintenanceExtractor、对应 Server/options 字段，以及 RiskObservationEnabled 配置读取和专属测试；普通 hooks/policy、用量统计及新 dreaming 执行器保留。主 Agent Agent runtime 全包普通测试通过（1.830 秒）。API 全包连续运行三次通过（合计75.783秒），未复现此前单次失败；首次失败原因仍未知，不宣称已定位修复。

条件审批与调度还在开发。本轮审查明确：审批完成必须通过持久化 CAS 防重复投递，不能在入队后提前清除消费者依赖的 pending delivery；需保留原消息正文及触发参数，不能恢复时用空 payload 重建任务。旧条件 runner 未上线，不保留旧/typed 双接口兼容分支。

### 默认触发器启动恢复隔离

reconcileAutoDefaults 跳过已标记 RecoveryRequired 的默认 trigger，保留原 pending delivery，不再因为单个待恢复 Agent 使整个 Node 启动失败。频率关闭也不在启动时丢弃该记录，其他 Auto 仍正常校正。恢复沿用已有显式 recover 请求，恢复后保持禁用；再按当前 Profile reconcile 恢复调度，不自动重放旧投递。

主 Agent API 专项 race 1.887 秒通过。真实重开存储/启动测试验证第二个 Agent 正常、off 的 pending 保留；调用 HTTP handler 验证错误 delivery 冲突、正确 revision/delivery 恢复、保持禁用及后续配置同步启用。测试未经过完整 onboarding 路由中间件，界面恢复入口尚待最终浏览器验收。

### 条件审批的调度持久化

ConditionRunner 统一使用带 trigger/delivery/session/Agent/revision/occurrence 的请求及 matched/not_matched/awaiting_approval 结果，不保留未上线的 bool runner 兼容分支。待审批保留 claim 与原任务正文、reason、payload；CompleteCondition 通过持久 CAS 防止重复投递，入队成功后由消费者确认 pending，提交不确定时不自动回滚重放。拒绝释放内存与持久 claim，允许后续检查；重启冻结记录必须先显式恢复，不允许通过 completion 绕过。

主 Agent Triggers 全包 race 1.987 秒通过，测试实际覆盖并发 completion 一次投递、manual nil occurrence、原任务参数保留、重开后拒绝绕过恢复、拒绝后下一次检查、旧 revision/无条件 pending 拒绝。随后补齐显式恢复清除 condition 元数据的回归。Manager 专项 race 3.855 秒通过，仍不等同于现有 HTTP 审批入口和生产 scheduler 已接通，后续接线保持进行中。

### 默认唤醒恢复入口

Auto 设置独立读取默认 trigger，按真实 recovery_required 显示新页面管理入口，当前草稿不因导航丢失；刷新状态与配置草稿分离。缺失 trigger 可同步重建，已恢复但禁用、频率不符以及关闭保存部分失败均保留同步入口。恢复面板发送准确 delivery_id 与 revision。dreaming recovery_pending 不触发此入口。

主 Agent 前端全量 64 文件、356 项测试通过，面板14项覆盖真实路由解析、状态隔离、404同步、503部分保存、关闭分支与草稿保留。浏览器视觉验收仍未进行。

### 自有自定义触发器与条件审批复核

自定义 user trigger 固定指向自身 Auto 主会话时应用配置的最大工具轮次，即使默认频率关闭也独立有效。普通 Agent 的主会话与其他会话 trigger 保持原行为。主 Agent 专项 race（provider 与条件 HTTP 链路）3.420 秒通过，轮次修复提交为 51d27bd8。

条件 HTTP 测试经过真实 Handler、注册 Agent 与 SQLite 会话存储，验证审批前无模型调用、错误批准参数保留待审批、批准执行一次并投递任务、拒绝不执行不投递。主 Agent Session/Turn 全包 race 分别54.677/12.241秒通过，Triggers 全包 race1.980秒通过，Tools 普通全包13.656秒通过。同期 API 全包失败明确指向自定义 trigger 校验影响普通 trigger 的用例；该项修正后专项通过，尚未完成修正后的全包重跑。

审核还发现恢复 runtime 缺少 completion 回调绑定，以及名为 submit failure 的测试实际只直接调用恢复存储函数，要求补足真实链路验证，不能用命名替代证据。Dreaming ASK 恢复缺少经验收尾关联，已进入修复；这些项目不得计为最终验收完成。

随后修正 Create/Replace 回调绑定，主 Agent 对 Manager 重建与 validator 专项 race2.967秒通过；修正后的 API 全包29.913秒通过。下游失败链路测试仍需补足。

### 外置手册搜索路径

修复 handbook 根目录在工作区外时 glob/grep 输出工作区相对路径的问题。结果统一使用可回读的 handbook/...，链接根目录使用 canonical root 计算路径，未绑定手册时保持普通工作区路径语义。主 Agent Handbook/Glob/Grep 专项0.398秒通过，修复提交为 ffc7ab44。相对配置目录仍锚定 Agent state root，绝对配置目录保持绝对路径。
