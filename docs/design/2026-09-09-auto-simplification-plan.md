# Auto 简化实施方案（当前开发目标）

日期：2026-09-09。用户已恢复开发并明确替换旧方案。本文件取代旧 Auto 员工 Goal/Cycle 方案；旧模块未正式落地，不实现旧消息、旧 Auto 状态迁移或双协议兼容。主 Agent 负责方案、审查和验收，Luna 负责应用编码。

## 1. 目标与明确删除项

Auto 是能够在同一主会话中定期自主工作的 Agent。职责直接注入 system prompt；Todo 承载待办；现有 trigger 驱动激活；可选每日 dreaming 整理经验和手册。复用原有审批。

删除旧 Auto 的岗位边界独立字段、计划模式、Goal/完成条件、业务 Cycle、独立自主会话、运行次数上限、累计和单轮 Auto token 预算、调度意图投影、独立事件源和探针体系、Auto 风险影子评估及其专属授权配置、旧维护父子 receipt/未知费用恢复结算产品链路。不仅隐藏 UI，必须撤除路由、工具注册、运行时调度和对应测试依赖。其他产品共用的 trigger、LLM 用量统计、原有审批和会话持久化不得误删；旧 Goals API 已确认属于旧 Auto，随之退役。

不删除用户历史记录或手册文件；不为其提供旧 Auto 消息语义适配，也不自动迁移旧 Goal 记录为 Todo。旧后台调度入口必须退出生产接线，不能残留唤醒。

## 2. 唯一产品模型

| 对象 | 内容与权威来源 |
| --- | --- |
| Auto 配置 | Agent ID、revision、职责、唤醒间隔秒数（0 为关闭）、单次激活最大工具轮次、dreaming 开关/本地时间/时区 |
| Todo | Agent 归属；条目稳定 ID、revision、内容、pending/in_progress/completed 状态；编辑和删除均执行 CAS |
| 经验 | Agent 归属、revision、正文和最后成功 dreaming 时间；独立于普通配置写入口 |
| 默认唤醒 trigger | 每个 Agent 唯一，受系统管理；频率设置为用户唯一编辑入口，trigger 列表可查看并跳转 |
| 主会话 | 聊天、自主激活共用一个会话身份和历史；dreaming 成功后更换活跃上下文起点，历史可查看 |

保存配置与默认 trigger 的同步必须可重试且幂等，不新增 ScheduleIntent 状态机。启动校正只维护这一条默认 trigger；关闭时取消尚未开始的默认唤醒。用户其他自有 trigger 独立受各自开关控制。

职责和经验在每个新 Turn 的 system prompt 中分别成段，使用快照固定到本轮结束。普通聊天和自主激活采用同一构造器。保存职责在下一轮生效；经验仅在成功 dreaming 后的下一轮生效。不能只注入 user/context 消息冒充已满足 system prompt 要求。

## 3. 激活与工具循环

频率选项：不自主激活、30 分钟、1 小时及少量预设、自定义正间隔。默认 trigger 到期，经已有脚本条件检查后，在 Agent 空闲时投递主会话。忙时重复唤醒合并一次，不积压队列。用户输入优先，工具实际返回前不并行启动另一轮。

激活时强制读取最新 Todo，并投递固定任务语句：

> 请结合此前对话、当前待办和可访问资源的变化，判断现在是否有值得推进的工作。有则在职责和现有授权范围内执行，并更新待办；没有则结束本轮。不得将未经检查的资源视为没有变化。

没有工作时仅保留可查看执行记录，不发送重复聊天通知。普通聊天也能获取最新 Todo。用户和模型通过同一存储与版本检查修改 Todo，冲突时读回后重试，不静默覆盖。

一工具轮次定义为一次模型响应发起的一批工具调用及其结果返回。达到上限后不再开放工具调用，允许一次不带工具的收尾。仅自主激活应用此限制；普通聊天沿用原有执行规则。不以缺失模型 token 用量阻断下一次激活；保留用量观察，不设 Auto 累计预算。

## 4. Dreaming

默认关闭，可配置每天时间与时区。每日最多一次成功提交；忙碌时延后，与主会话串行。输入为当前上下文、旧经验、Todo 及按需读取的手册。模型归纳经验，长内容落到既有手册目录，经验字段只保留必要摘要和索引。

普通聊天/激活工具没有写经验能力；用户配置 API 也不能写经验。只有可信 dreaming 执行上下文允许提交。经验存储与普通工作区隔离，不能靠提示词宣称写保护。dreaming 的手册写入仍走原有审批和路径规则。

顺序：完成文件写入→保存新经验→持久化上下文重置起点。保存失败不得清空上下文。以最小可恢复提交标识处理重启窗口，避免重复清空后续用户消息；不恢复旧父子费用结算体系。失败保留旧经验，明确显示失败；待办不被清空。上下文重置保留会话身份和可查历史，不物理删除消息。

## 5. UI 与 Manage

- 侧栏保留智能体/工作组/自主智能体三组，统一行高缩进，使用轻量状态点；条目更多菜单在选中/悬停显示，键盘亦可访问。
- 移除侧栏底部自主任务设置按钮；底部只保留全局连接与设置。Agent 配置从当前页菜单和智能体设置进入。
- 设置页一配置一行：职责、自主激活频率、最大工具轮次、每日 dreaming；时间设置按开启状态展示，经验只读展示最后更新时间。
- Todo 在主聊天页可展开查看和编辑；不再出现目标/验收/周期/累计预算/核对结算入口。
- 总览只显示 Agent 状态、下次激活、待办摘要、最近活动和 dreaming 状态。Manage 同步这一摘要，移除旧 Goal/Cycle/预算恢复 DTO 依赖。

## 6. 开发顺序与验收门槛

1. 新模型与存储：配置/Todo/经验、Agent 隔离、CAS、原子保存和失败回滚；不接旧 Auto 兼容。
2. 主会话链路：职责与经验真实 system prompt 注入，Todo 最新读入，共用聊天与激活上下文，默认 trigger 同步与单轮工具上限。
3. Dreaming：权限限定、手册长文、成功后上下文重置、失败及重启边界。
4. 删除旧链路并改 UI/Manage/OpenAPI/用户文档，不保留双实现供生产选择。
5. 真实配置 LLM 验收及 Node/Manage 全页视觉验收，最后确认旧调度不再运行。

必须证明：默认 trigger 修改/关闭/重启不重复；同一 Agent 无并发模型循环；激活能看见用户刚修改 Todo 和前次聊天；用户能在自主上下文中纠正任务；轮次上限真实终止工具调用；职责与经验位于实际请求 system prompt；普通工具不能写经验；dreaming 失败不丢上下文、成功后历史可查但旧正文不再送入模型；跨 Agent 无越权；既有审批仍实际生效；无工作不刷屏；桌面/窄屏布局协调。

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
