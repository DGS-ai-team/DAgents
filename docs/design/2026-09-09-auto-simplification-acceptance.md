# Auto 简化方案验收记录

本记录对应 [开发方案](2026-09-09-auto-simplification-plan.md)。自动化测试使用本地测试模型，不能替代真实提供商和浏览器验收。

## 已有自动化证据

| 要求 | 证据 |
| --- | --- |
| 职责进入主会话系统提示；Todo 用户与模型共用 | `TestAutonomyV2SavedResponsibilityReachesMainSession`、`TestAutonomyV2TodoToolRunsThroughMainSession`，所属 API 全包通过 |
| Todo 归属与版本冲突，经验不能由普通配置写入 | `TestAutonomyV2TodoAgentIsolationCASDeleteAndExperienceReadOnly`，API 全包通过 |
| 默认触发器与自有自定义触发器的激活上限 | `TestAutonomyV2TriggerRoundProviderRequiresExactDefaultDelivery`、`TestAutonomyV2TriggerRoundProviderCapsOwnedUserTrigger`，专项 race 与 API 全包通过 |
| 条件脚本使用原审批入口 | `TestConditionTriggerHTTP…`，经过真实 Handler、注册 Agent 与 SQLite，会话恢复、错误参数、批准/拒绝均有断言 |
| 条件投递失败不自动重放 | `TestConditionCompletionCallbackFailureMarksRecoveryAndReopenFreezes`，真实失败 submitter、持久冻结、重开后检查；专项 race 通过 |
| Dreaming 多次审批、拒绝、取消 | `TestRunDreamingResume…`、`TestRunDreamingWaitingCancelPersistsCancelledAttempt`，真实 Manager/审批路径，专项 race 通过 |
| 审批后一次提交经验并清理活跃上下文 | `TestDreamingSchedulerASKResumeFinalizesAndAcksAttempt`，验证 revision、ResetApplied、Ack 和同日不重跑；调度全组 race 通过 |
| 历史保留、模型只使用活跃片段 | `TestResetActiveContextKeepsHistoryAndIsIdempotentAcrossRestart`、`TestResetActiveContextNextModelUsesOnlyActiveTail` 及真实会话集成测试；Session 全包 race 通过 |
| 审批等待期间重启 | `TestDreamingWaitingApprovalSurvivesRuntimeRestart`，SQLite 关闭重开后继续原审批；专项 race 通过 |
| 手册外置目录搜索路径可回读 | `TestExternalHandbookGlobAndGrepUseHandbookNamespace` 与链接根目录测试；Tools 专项及全包通过 |
| 旧产品入口退出生产 | `TestLegacyAutoRoutesAndToolsAreRetired`、`TestRetiredGoalCheckpointIsNotRegisteredOrCallable`；旧 API 返回 404，旧工具不暴露且不可执行 |

## 批量回归范围与限制

- Node 批量运行中，API、Session 和其余包通过，Tools 的进程退出等待发生超时；Tools 随后完整分包复验通过。
- 并行 runtime race 中，Session 的 post-clear 聊天等待超时；Turn、Triggers 通过。Session 随后完整分包 race 通过。
- 两个首次超时均保留诊断，不宣称根因已定位，也未为通过测试放宽生产约束。
- Node 与 Manage 前端构建通过。Manage 保留第三方 eval 与大 chunk 构建警告。
- 恢复孤立 dreaming 记录时先恢复持久消息队列再保存。重启、审批等待、取消及排队用户消息保留专项 race 6.685 秒通过；最终 DreamingScheduler 全组 race 3.357 秒通过。

## 当前验收结论（2026-09-10 更新）

下文按时间保留原始证据与失败记录，早期“待验证”不代表当前状态。整体目标仍在进行中。

| 要求 | 当前证据与判定 |
| --- | --- |
| 单主会话、职责/经验 system prompt、最新 Todo、CAS 隔离 | API/Session请求捕获和隔离测试通过；真实mimo普通聊天及默认激活更新Todo通过 |
| 工具轮次上限及既有审批 | 真实激活一次工具调用后无工具收尾、诚实报告未完成Todo；原ASK实际批准、dreaming拒绝/取消专项通过 |
| 默认trigger稳定身份、关闭、忙时合并、恢复 | 生产provider专项、关闭清队列及重启冻结测试通过；隔离实际页面、CAS冲突、原生确认、同步唤醒均通过；完整进程重启后实际到期一次的黑盒证据仍待补强 |
| Dreaming与文件系统手册 | 真实mimo经验提交、上下文重置、历史保留和独立Agent手册读写通过；失败/重启组件测试通过；真实进程故障重启组合未独立实测 |
| 无工作静默 | 真实默认激活调用auto_idle，执行记录完整，notify_seq不变 |
| 脚本条件 | 真实Handler/SQLite/原审批测试覆盖批准、拒绝及失败冻结；`fa159b41` 已记录 18766 真实 `exit 1` false 对照（批准、skipped、无额外模型回合、临时 trigger 清理）；真实 true 条件触发模型本轮未重做 |
| 旧架构退役 | Goals、events/probe、maintenance runner、风险闭环专属包删除，旧入口不可调用；全Node普通测试通过，保留共享存储原语和历史文件 |
| Node/Manage视觉 | 已完成多批桌面/窄屏截图及发现问题的修复；详情剩余区段、Manage剩余模块窄屏仍需按页补齐，不能声明全页完成 |
| 构建与回归 | Node前端369项、Python197项通过；两端构建通过。历史超时及第三方Windows race限制保留 |
| 部署与交付 | e1c18cb0链接修复已切入18766，HTTP200；最终交付时仍须打开Node/Manage并确认工作区干净 |

## 2026-09-09 真实运行验收补充

- 新版 Node 已原位切换到本机 18766，沿用原 runtime 与已配置 mimo-v2.5-pro；Manage 8022 已重启并通过管理员 UI 登录。
- 真实模型普通聊天调用 todo_create、todo_update，经两次原审批生成 completed/revision2 的“验收：67×19=1273”。
- UI 保存职责、60秒默认激活与3轮上限。实际 scheduler 自动触发一次，主会话显示固定激活消息，模型读取新待办并经原审批更新为“验收：71×17=1207”，completed/revision2。验收后频率关闭，默认 trigger fire_count=1、target_session_id 与 Agent 主会话相同。此项不单独证明已触及工具轮次上限。
- 本地 Manage node token 原绑定测试别名，与实际 UUID 不一致；修正绑定后注册、心跳、Auto 摘要上报返回200，WebSocket连接成功。未放宽角色或关闭鉴权。
- 内置浏览器检查发现 Auto 原生控件/待办标题及侧栏入口不协调，修复已提交e37853aa；Manage旧缓存导致总览500，严格读取隔离修复f64bf9e2，10项Python测试及登录后页面空状态复验通过。
- 新发现前端重启重连审批显示延迟、模型修改Todo后面板陈旧，仍在修复。真实dreaming、全页及窄屏视觉验收尚未完成。以上取代前文“真实LLM和新版环境均未开始”的旧状态，不扩大已验收范围。

### 真实 dreaming 与后续聊天

2026-09-09 23:53 的真实 dreaming 成功提交经验revision1。SQLite只读检查：历史26条、active_context_start=26、reset ID=dreaming:agt-9ea36189d2221ea1:2026-09-09、dreaming attempt已Ack。随后浏览器发送普通聊天并收到“重置后聊天：1027”，上下文显示58 tokens，原历史仍可见。验收后每日dreaming关闭。

内容质量未通过：此次摘要将历史已退役工具和未经核实的手册路径写入经验。正在补强通用整理规则，以当前能力和已验证路径为准；不新增旧消息兼容。此次模型未选择手册落盘，因此不能计为真实手册写入验收完成。

Todo重新展开及当前回合结束刷新已提交aa85cdff，主Agent专项10项测试通过。重启后审批显示延迟尚未充分复现定位，不宣称已修复。

### 桌面布局与 dreaming 规则复核

内置浏览器1280×720查看通用设置（含底部）、MCP空状态、Linux通道空状态、能力上下分区、技能空状态：未见重叠、横向裁切或字段挤行。只覆盖上述实际可见状态，不覆盖已配置条目或窄屏。

dreaming通用提示已修正为当前工具名称与绑定手册根目录为准、未验证路径不记忆、淘汰过时规则、明确最终输出将持久化；职责仍只注入system prompt。主Agent完整DreamingScheduler专项race3.373秒通过，测试捕获最终模型请求。真实模型复验尚未执行，不能宣称提示规则能保证消除幻觉。

### 2026-09-10 修正提示后的真实整理

每日00:03真实dreaming再次成功，经验revision2，旧autonomy工具未再作为现行流程保存。但模型说明当前只暴露Todo工具，无法创建请求的qa/todo-workflow.md；检查手册目录没有新增文件。因此仅确认当前能力约束生效，不宣称手册落盘通过。经验仍含待执行事项与不完整路径索引，质量限制保留。每日dreaming随后关闭。

输出防护与上下文页上下区域已做1280×720只读视觉检查，未见错位，未改动防护设置。手册写入下一步需在验收Agent配置文件能力并保留原审批。

### 新版切换与管理台上报复验

2026-09-10，当前 Node 使用包含51f212d4的 dagents-node-stream.exe，18766健康检查正常；5173页面刷新后恢复完整回复。重启后不刷新页面的真实复验仍失败：后台6秒完成，旧页面未收到事件。日志表明重启后没有新SSE订阅，故服务端游标回退修复尚不足以解决半开连接，客户端失联检测继续修复。

验收Agent已通过设置页启用文件能力并保存，未启用命令或网络工具，原审批保留。此前两次dreaming没有文件能力，不能据此宣称手册写入通过。

Manage重新登录后，1787像素桌面截图检查首页和有数据的Auto总览：注册实体显示2，Auto摘要显示真实Agent、Node UUID、激活关闭与dreaming关闭，上报时间持续更新。未见布局重叠；两条已完成Todo仍显示“2项待办”，计数文案待修正。该项不覆盖窄屏或其他管理模块。

### 真实手册写入闭环与连接复验限制

新增私有工作目录的Auto验收Agent `agt-86cd2b08565d2c10`，使用已有mimo-v2.5-pro，仅启用文件工具。普通聊天完成83×17=1411，配置每日00:28 dreaming、8轮上限。真实调度执行glob_files、write_file、read_file；在18766内嵌UI批准唯一写入后，文件实际保存于该Agent绑定手册的qa/arithmetic.md，模型读回。00:30:57经验revision1提交，SQLite只读检查历史12条、active_context_start=12、reset ID为该Agent当天dreaming，attempt已Ack。验收后关闭每日dreaming（配置revision3），默认激活始终关闭。

此次证明真实文件写入、原审批、读回、经验提交和上下文重置；未触及8轮上限，不能据此证明轮次触顶。经验保留了工具别名handbook/qa/arithmetic.md及操作摘要，包含对一次绝对路径失败的概括，不将其提升为所有路径行为的产品保证。

776038db新增命名心跳与45秒失联检测、5秒重连、旧连接回调隔离，并修正Node/Manage未完成/已完成计数（Node保留文字摘要）。主Agent23项前端测试及Node构建通过；Luna专项Go race与Manage构建通过。新版watchdog二进制已在18766运行。5173旧页面在重启后曾补回消息，但仍有连接/审批延迟；关闭临时重复标签页后恢复在线，尚不能排除连接上限或其他订阅问题。改用18766内嵌UI后本次审批与结果正常显示，此项不证明5173重连问题已解决。dreaming执行中状态接口仍显示waiting，另行排查。

### 工具轮次触顶与窄屏验收

1191e934修复活跃dreaming attempt的状态投影，已构建切入运行环境；不能以此代替审批等待状态的运行复验。

真实mimo模型在max_tool_rounds=1的默认激活中，经原审批只执行一次todo_create，新条目保持pending/revision1，未执行依赖其返回ID的todo_update。随后加入26cde8e2的无工具收尾指令再验：本轮只完成一次todo_update（条目a0105827…变为completed/revision2），指令待办d2226e09…仍pending/revision2。默认trigger累计fire_count=2，每次收到审批后均关闭频率以防再次激活，最新配置revision7、激活与dreaming均关闭。两次均证明执行层限制生效；两次最终文本仍含伪tool_call标签，因此收尾体验未通过。继续核查ASK恢复后真实请求是否包含收尾指令，不将原因直接归于模型。展开中的Todo面板在两次回合结束后均自动刷新。

390×844深色Node聊天截图发现待办原生控件白底，已修复为主题色、窄屏文本整行、状态与保存/删除同排，展开区域限制高度并内部滚动；构建切入后截图通过，主Agent8项Todo组件测试通过。Auto设置页上中下区域（职责、频率、轮次、经验、手册）未见横向裁切；导航白色滚动条已改主题色，构建后的390宽截图已复验通过。帮助与反馈深色窄屏上下区域也已检查，提交按钮与空历史可达且无重叠。

390×844 Manage浅色截图检查反馈空状态、Node列表及Auto移动卡片；Node列表通过局部横向滚动访问表格，反馈无重叠。Auto卡片的名称、Agent ID、Node ID已分行，长ID折行，构建后截图通过；未完成/已完成数字与实际Todo匹配。本轮不覆盖其他Manage模块或深色。

并行直接读取18766与5173 SSE：两端首条connected约0.03/0.04秒，命名heartbeat均15.03秒到达，均为text/event-stream分块响应，无Content-Encoding。未发现代理缓冲证据；尚未解决多标签页的实时延迟问题。

### 请求级收尾复验通过

66835a64补齐trigger-origin及dreaming的selection审批恢复请求捕获，并在最终无工具请求末尾加入不落持久历史的收尾指令。主Agent三项专项race通过（3.771秒）。新版summary-tail二进制原位启动后，真实模型在一次todo_create审批后创建“收尾验收：97×11=1067”（7866a055…，pending/revision1），00:58:16以自然语言列出已完成创建、尚未更新状态以及工具轮次已达上限；没有伪工具调用。此项证明实际默认激活+审批恢复+工具触顶+正常收尾链路通过。

本次审批等待跨过下一次60秒到期，默认trigger累计fire_count从2到4，曾有一条排队输入；关闭频率后，原turn收尾结束，hydrate为active=false、queue_pending=0、pending_hitl=null，未启动额外模型回合。该现象与“当前工作加一条合并的待处理唤醒”相符，不能单凭fire_count增长宣称重复执行或无并发证明完成；多次到期与关闭取消仍需结合生产链路专项验证。当前验收配置revision9、默认激活和dreaming关闭。

5173原页面未刷新，在现有少量标签页情况下收到真实模型“5173消息同步正常”，后台00:53:20完成。只确认此范围恢复，多标签页连接限制不扩大为通过。

需求审计另发现“无工作不重复通知”缺少明确生产判定和专项证据，现有固定激活语句只要求无工作结束。本项仍未完成；不得仅凭没有实际工作或模型输出某个中文短语就抑制通知。

## 2026-09-10 补充审计：触发器与设置页

在当前运行的 summary-tail Node（18766）以 1280×720 深色界面进行实际截图检查：

- 定时任务页仍将已禁用的旧 Goal trigger 展示为“自主任务托管”，并提示去当前 Auto 设置查看。代码中仍存在 `isManagedGoal` 和 `ValidateOwnersWithGoals`。这不证明旧任务仍会运行，但证明旧产品识别尚未清理完成；需要撤除旧控制器接线，保留历史时明确其已退役，不映射为新 Auto。
- 定时任务页及关于页的嵌入面板均有整宽空灰色栏。对应面板在 embedded 模式隐藏了标题内容，但仍渲染 header；已交由 Luna 修复并保留非嵌入弹窗头。
- 忙碌时允许当前回合加一条合并唤醒；不能仅凭 fire_count 增长判定重复执行。生产 Manager/Scheduler 的多次到期、关闭后取消排队专项测试正在补充。
- 无工作通知需要可信结束信号，不能通过自然语言猜测。已安排受验证 Auto 激活专用的最小控制信号实现，普通聊天、写入、错误及审批不得借此静默；尚未完成，不能作为已验收能力。

### 补充审查结果（同日）

开发前端 5173 的 1280×720 浅色实际截图确认：关于页与定时任务页的空 header 灰条已消失。此证据针对工作区前端，18766 内嵌资产尚未重新部署此批修改。

忙碌专项测试强化为必须实际存在一条排队投递后，root 执行 `go test -race ./node/internal/api -run TestSchedulerBusyAutoWakeupsCoalesceAndDisableDoesNotRunQueued -count=3` 三次均失败（queue=0、active=true）。检查证明测试漏接 `SetTriggerDeliveryTracker`，`NewScheduler` 本身不会完成该连接。因此撤回原测试可证明关闭排队取消的判断，需补生产同构连接后重验；这不是生产调度缺陷的证明。

UI 审查也发现当前 Auto 默认 trigger 仍被通用编辑入口当作用户 trigger。计划要求频率配置为唯一编辑入口，因此必须区分当前 Auto（托管、跳转设置）、用户 trigger（可编辑）和退役/未知控制器（只读不可执行），不能以“当前 Auto 可编辑”的测试断言作为验收标准。

### 忙碌队列验收补充

最终专项测试已改为真实用户聊天进入 ASK waiting，再由 Scheduler 多次投递默认 Auto trigger；不手工清除 pending。Manager 在 runtime 创建前绑定真实 trigger store。断言实际只存在一条排队唤醒、等待期间模型调用一次；关闭默认频率并恢复原 ASK 后，总模型调用两次（原回合的前后两次请求），队列清空且 runtime 空闲。root 独立执行上述专项 race `-count=3` 通过。这证明用户回合忙碌期间的唤醒合并及关闭丢弃，不单独证明 Auto 起源等待审批、重启恢复或 provider 配置校验。

### 旧调度入口与默认 trigger UI 收口

本批删除 Scheduler 的旧 Goal managed-fire、独立 event poller 和 reconciler 接口，Store 不再保留 GoalRef/ValidateOwnersWithGoals，启动校验统一为当前 user/auto controller。公共 fire 内部入口拒绝退役/未知 controller 及旧 managed 关联，即使 force=true 也不得投递；原条件脚本 runner 保留。默认 Auto trigger 的通用编辑、启停、删除和手动执行入口移除，改为跳转所属 Agent 自主设置；用户 trigger 保留原操作，退役项仅查看历史。

root 独立验证 `go test -race ./node/internal/triggers -count=1` 通过；TriggersPanel/UpdatePanel 专项 Vitest 共4项通过，覆盖卡片操作区别、实际设置路由及嵌入/弹窗头。该批尚未部署到18766。session 内未引用的旧 Goal 输入函数仍需另行移除，不能据本批认定全部旧字段清理完成。

### Auto 起源忙碌与旧输入入口收尾

新增 Auto 起源专项使用 NewServer 的真实 triggerToolRoundProvider、autonomy profile 与 trigger store；仅模型执行器注入测试客户端，避免外部网络依赖，测试不覆盖生产 LLM 工厂。实际默认触发进入 ASK 后验证合法 delivery 的轮次配置与错误 delivery 拒绝，多次投递期间模型请求数保持1，关闭后恢复原 ASK，最终严格为2次请求、无活动回合且队列清空。root 将该测试、用户起源忙碌测试及既有 trigger 审批收尾测试一起运行 race `-count=3`，API 与 Session 均通过。

同时移除 session 的未引用 SubmitGoalTriggerMessage/EnqueueGoalTriggerMessage 及共享入队函数中的旧 Goal/Run 参数；普通 trigger 与 Auto 入队接口保留。现存历史字段不因此删除。静默结束仍处于独立开发审查中，本批不将其视为通过。

### 默认触发器跳转实测与 dreaming 历史显示缺口

5173 开发前端 1280×720 浅色截图确认当前两条默认唤醒卡片仅提供 Auto 设置和历史入口；实际点击第一条“打开 Auto 设置”正确进入 agt-9ea36189d2221ea1 的 autonomy 分区，未改动配置。卡片与设置页未出现重叠。

同一设置页显示已有长期经验正文，但每日 dreaming 关闭后“上次成功”仍为“暂无”。关闭调度不应隐藏成功历史，此项已交由 Luna 核查修复，尚未验收。该观察与静默结束实现分别跟踪。

### Dreaming 历史成功时间修复验证

CurrentStatus 统一附带最近一次 ResetApplied 成功记录，保持当前的 waiting/running/failed/recovery/disabled 状态与调度时间语义。新增查询只读取该 Agent 的完成记录；未提交完成的 dreaming 不算历史成功。root 执行 API/autonomy 的 Dreaming 专项 race 通过，测试验证次日等待、关闭、失败、运行中与重新打开持久存储后的准确成功时间。

Manage Auto 副标题同步改为当前状态、待办与最近上报的准确描述，Manage 前端构建通过。上述 Node 状态修复尚待新版进程的实际页面复验；无工作静默机制的整体回归与通知历史验证另行进行。

### 静默结束的回归与未通过项

root 已运行 Session、Turn、Policy 全包 race，通过；Tools 与 shared/config 全包普通测试通过。静默结束正向工具可见性、只读继续执行和反向伪造/写入/审批测试已有初步证据，但不能据此认定持久化闭环。

强化“本次 idle 工具结果必须保存”断言后，`TestTrustedAutoIdleSuppressesNotifyButPersistsHistory` 在 root 定向 race 中失败：激活输入与工具调用存在，对应工具结果未被验收读到。普通聊天历史存在并不能替代该证据。当前正在区分生命周期提交时机与真实漏存，静默结束仍不得提交为已完成或部署。

另已补回默认 Auto trigger 的旧投递恢复入口，仍保留配置从 Auto 设置修改的限制；恢复携带 revision 和 delivery ID，旧/未知 controller 不可恢复。root Recovery/Recover/RetiredController 专项 race 通过，前端最终确认文案待复验。

默认唤醒恢复的最终确认提示已区分 Auto 与普通触发器，root TriggersPanel 三项测试通过；Auto 使用原 CAS 恢复 API，恢复后引导回自主设置同步。此修复不开放默认触发器的通用编辑或手动启用。


### 2026-09-10 静默结束持久化修复与真实模型验收

此前严格测试失败的根因是生命周期工具事实未填写 ResultContent，运行时消息虽有结果，事件持久化记录却缺少正文。现已为工具完成及结果记录补齐正文；静默完成事件只在生命周期成功提交且回合 completed 后发布。root 完整 Session/Turn/Policy race 通过（61.497/11.391/1.270秒），随后精确运行 TestTrustedAutoIdleSuppressesNotifyButPersistsHistory race 再次通过（2.271秒），覆盖实际通知序号、输入、调用、结果及重新打开持久存储后的历史。

01:48 将18766切换至最新 idle-completion 构建，PID17228，健康检查通过。01:50:21 原验收 Agent 的默认 trigger 实际投递，真实 mimo-v2.5-pro 于01:50:25调用 auto_idle（call_ff27fa426a59446f920f2669），无审批、无写入；完成前后 notify_seq 均为485。只读 SQLite 核对末尾记录包含激活输入、对应 assistant 工具调用及成功的 {"no_work":true} 工具结果。随后关闭验收频率（revision8），active=false、queue_pending=0、pending_hitl=null。此证据证明真实默认激活的无工作结束和通知抑制，不扩大到所有错误恢复场景。

新版 dreaming API 在 disabled 状态仍返回 last_success=2026-09-09T16:04:18.0963628Z。实际 Node 深色桌面截图显示连接在线，三组侧边栏和聊天输入布局正常；全部 Node/Manage 页面的视觉验收仍未完成。另观察到工作组连接持续返回4401，需进一步核查，不能宣称所有 Manage 通道正常。

补充：root 将 SQLite 关闭故障注入测试与正常持久化测试共同运行 race，通过（7.448秒）；该故障发生于模型返回工具调用时，证明这一持久化失败路径不发布成功 no_work，未覆盖每个提交阶段。新版自主设置实际截图确认关闭状态下仍显示上次成功 2026/9/10 00:04:18；同时发现长经验撑高整行且标签垂直居中，另行交由 Luna 调整。验收监视脚本已正常退出并再次确认频率关闭、无活动/排队/审批、notify_seq=485。

### 2026-09-10 补充视觉与计划审计

root 复验 SimplifiedAutoPanel 15项前端测试通过，并在5173浅色1280×720实际查看上下半页：经验标题顶端对齐、正文限高内部滚动，下方保存按钮与手册目录可发现。修复提交2f9748dc；新版内嵌资产待随下一次Node重启部署。Manage深色1787×1216实际检查能力市场空态、上传Skill弹窗、配置LLM空态，未见重叠或裁切；未上传或发布任何内容。这不覆盖有数据详情及窄屏。

工作组WS未带Node token的缺口已定位，修复的Workgroup全包race通过，仍待真实Manage连接复验。旧Goal/Risk注入链默认未启用但存在残留可执行分支，继续清理；旧trigger拒绝栅栏须保留以防历史记录重新执行。

### 工作组鉴权代码验收与发布状态

91a70c4c 将工作组WS接入已有Node Manage token；拨号读取单一provider。生产provider捕获启动时凭据，setup修改要求重启，不宣称热轮换。root在最终单一provider代码上运行Workgroup全包race，通过（3.793秒）。真实Manage连接尚未复验，当前运行服务仍为idle-completion构建。共享工作区正在清理旧Risk注入链，中间态存在编译错误，须待清理完成后构建，不能误报最新修复已部署。

另实际检查Manage深色桌面的版本发布表单及案例库空态，未见重叠；未上传安装包、未发布版本、未创建案例。窄屏及非空详情继续保留为待验收。

### 已提交版本隔离部署与真实WS复验

为避免把旧代码清理中间态带入服务，root从0aae75cf执行git archive到独立临时目录，复制已构建的当前前端静态资产，成功构建ws-auth-committed二进制。确认两名验收Agent空闲、无排队/审批后，02:01:05启动Node PID30348，沿用原数据目录和进程配置，health正常。

Manage日志显示该进程本地端口49972的WS被接受；随后出现每15秒的subscribed_by/acl_member工作组列表查询。源码确认sendResumeOffers及15秒刷新只在收到session.welcome后执行；再次检查49972→8022连接仍Established。重启后的日志未再出现4401。这证明真实凭据握手及订阅刷新闭环，不代替非空工作组任务投递验收。长经验布局资产已一并部署，用户Node页已刷新。

### 旧Goal执行上下文与风险影子接线清理

移除queue.Envelope的旧GoalID/RunID投递字段、session持有/传播、工具Goal上下文及旧child-tool特判；移除RiskSubmitter从Agent构造、TurnOptions、runtime到orchestrator的注入、关闭和提交链。对应旧功能专属测试删除，普通policy/ASK、usage、child-agent通用检查及真实历史消息结构保留，旧trigger拒绝栅栏未移除。

root完整Session/Turn/Policy race通过（65.615/10.880/1.272秒），Tools/agentruntime普通全包通过（13.496/1.826秒）。随后queue字段删除的最终版本通过Queue race（1.576秒）及API编译检查；API本轮仅编译，不声明完整API回归。尚未清理goals/events/maintenance等无生产调用的底层包，本批不作为旧架构全部删除的证明。运行服务仍为已提交WS修复版，不包含本批清理。

### 旧独立事件源删除及本轮未决项

删除无生产接线的node/internal/events probe实现、专属测试/设计文档及Registry孤立字段；保留现有trigger脚本条件、SSE和历史事件存储。events.json退役文件不阻断启动的回归用例保留，不建立数据迁移。root Startup/Retired专项普通测试通过，Triggers全包race通过（2.037秒）。

Luna运行API全包时报告Auto起源忙碌测试一次PendingDeliveryID为空；root随后精确执行该测试race三次通过（4.369秒），尚不能解释首次失败，继续诊断且不归因于既存问题。

真实18766工作组空页截图发现：提示点击隐藏的+新建，且未选择工作组被表示为实时离线。已安排复用既有创建入口、准确区分未选择与连接错误；本项尚未完成。

### 工作组空态界面复验

root NavRail三项测试通过；5173浅色1280×720实际截图显示直接新建按钮及“实时事件：未选择工作组”，点击新建实际打开NavRail已有名称/取消/创建表单，未创建数据。移动端处理补充先展开导航再nextTick打开原表单，构建通过；尚未完成窄屏实际截图验证，因此不扩大桌面证据。选中工作组仍使用原有SSE状态。

### 工作组窄屏入口与忙碌测试收尾

root在5173设置390×844视口实际验收：工作组空页主按钮完整可见，点击后移动导航自动展开，原有名称输入、取消/创建表单可见，无横向溢出。未创建数据；验收后恢复默认视口并关闭临时标签页。这补齐699270a0的窄屏交互证据，仍非整个产品全页窄屏证明。

9f0974f6将Auto起源测试的合法/错误delivery检查放在受控provider阻塞窗口，释放后继续ASK/合并/关闭不重放断言，取消与cleanup可安全解锁。root最终精确race count10通过（12.013秒）。首次失败由测试在claim清理后检查造成，不修改生产投递语义。

### 旧维护执行器删除验收

删除无生产调用的memory maintenance reader/runner及session RunHandbookMaintenance旧执行器和专属测试。dreaming_test依赖的三个测试客户端移至独立辅助文件，保留原dreaming断言；初次编译缺失辅助的失败已修正。root Memory/Session/API 的Dreaming|Maintenance|Handbook|Context专项race通过（7.509/27.418/6.475秒），删除入口名称不再有生产/测试引用。新版execution gate、dreaming调度、上下文重置、手册文件工具与历史存储保留。此批仍未移除goals包的旧存储模型，也未部署到运行Node。

### 旧Goals底层闭包删除

删除node/internal/goals及hooks旧risk worker/observation/Goals适配和专属测试；非测试入向检索无剩余goals依赖，不新增旧数据decoder或迁移。磁盘历史文件不删除。root Hooks/Autonomy/Triggers全包race通过（2.000/1.311/1.878秒）；Node全部包编译检查通过，命令使用-run ^$，不作为全量执行测试证据。删除前当前API全包普通回归通过（27.881秒）。

Manage深色桌面非空工作组卡片、通用配置和Supervisor配置已实际检查。创建仅用于验收的草稿wg_27f7e34c3e333be286ec5ebd78，未发布或发起任务；浏览器归档确认框无法由当前浏览器工具操作，随后使用同一管理员登录及既有archive API清理，当前状态archiving，未确认最终archived。浏览器确认框和归档完成仍需收尾；窄屏重载回首页，未将其误记为工作组配置窄屏通过。

### 临时工作组归档完成

核查WorkgroupStore.begin_archive发现首次调用仅转archiving，第二次才转archived，当前没有自动完成接线。root确认目标仍是本次临时草稿后完成第二阶段，GET核验status=archived；新开管理台页面显示工作组0/0。已安排修复配置中草稿的单次归档幂等行为，不将当前两次调用当作产品闭环。旧标签页原生confirm仍被浏览器工具阻塞，新管理台标签24已打开且可正常操作。

### 清理后整体测试及服务更新

root执行go test ./node/... -count=1 -timeout=180s，Node所有包测试通过（含API35.440秒、Session34.102秒、Tools16.639秒），不等同Windows全工具race。随后构建clean-auto版本并确认两名Agent空闲后更新Node PID23532；health正常。

720617b3修复配置中工作组单次归档并保持archived幂等，活跃组原流程未改。root Store/API共12项测试通过，重启Manage PID27932后Node自动重建WS且订阅刷新/心跳/摘要上报均200。通过真实API创建草稿wg_91ff916b44be3a1c55f8df1bc8，首次archive即返回archived，GET读回同状态。两条临时验收草稿均已归档，无验收任务运行。活跃工作组完整归档流程不据本批声明通过。

### 当前核心逻辑审计与新增窄屏覆盖

按当前代码重新核对：AgentPromptProvider→session→turn将职责/经验放入实际ChatRequest.SystemPrompt，并冻结本轮、下一轮刷新；Todo请求级最新读取及用户/工具CAS共享由agent_prompt_provider与autonomy_v2专项覆盖。默认trigger保存/关闭/重建/冻结有autonomy_default_trigger_api专项。dreaming在CommitDreaming后才ResetActiveContext并标记/确认，保留失败、审批恢复、孤立attempt和队列恢复测试；不再引用删除的旧maintenance实现作为证据。真实进程重启后默认trigger再次到期的黑盒证明仍可补强，不能将组件重建测试说成该场景实测。

root实际18766深色390×844检查通用设置上下半页、MCP空态、Linux通道空态。后两页未见裁切或重叠；通用页运行状态两卡过窄造成标签与状态换行，已安排最小布局修复。验收后恢复默认视口并关闭临时标签页。本批没有更改任何配置值。

### 追加Node窄屏视觉验收

root在当前18766深色390×844依次查看能力上下半页、技能空态、输出防护上下半页、上下文上下半页。前三项字段、长说明、操作按钮无裁切或重叠；上下文页“当前对话诊断”的“查看”操作被压成两行，已交Luna局部修复。未改配置值，结束后恢复默认视口并关闭临时页。此前6feca467状态卡片修复在5173浅色390×844已实际确认两列均分宽度、服务状态单行；内嵌服务尚未部署该单独CSS提交。

### 2026-09-10 最新 Node 部署与窄屏复验

- 重建 Node 前端和 Go 二进制，切换 18766 到 `dagents-node-latest.exe`；保留原运行目录、环境、配置与会话。切换前两台验收 Agent 均无活动轮次、排队任务和审批；切换后 UI HTTP 200，资源指纹与本次构建一致。
- 在实际嵌入式 UI、390×844 暗色视口复验 ContextSettings：诊断说明正常换行，“查看”保持单行，箭头不被压缩。局部 scoped 样式修复验收通过。
- 同一版本模型与连接页上、下区域截图检查：模型卡片、保存按钮、Manage 地址及工作组开关均位于容器内，无重叠与截断。本轮未修改连接配置。

### 2026-09-10 最终回归批次

- root 执行 `npm test --prefix node/webui/frontend -- --run`：66 个测试文件、369 项测试全部通过。Vue 编译器对 const reactive v-model 的自动转换提示仍存在，未造成测试失败。
- root 执行 `py -3.13 -m unittest discover -s tests -p "test_*.py"`：197 项测试全部通过（43.315 秒）。
- root 执行 `npm run build --prefix manage/console/frontend`：构建通过；第三方 page-agent eval 与产物体积提示仍存在，不将构建通过等同于这些提示已消除。
- 同步开发方案中的旧架构删除和工作组 WS 鉴权状态；默认 trigger 恢复实际界面及剩余视觉范围仍未宣告完成。

### 2026-09-10 关于页与智能体列表视觉复验

- 最新嵌入式 UI 智能体设置列表：桌面与390×844暗色截图中，筛选、Agent配置入口及模板区域布局协调。
- 关于页原四列状态卡片在390px下造成“当前版本”和平台名称碎行。UpdatePanel局部媒体查询改为两列等宽、卡片min-width:0；5173浅色390×844真实截图确认标签及windows-amd64均完整单行。Luna前端构建通过；本项尚待并入下次嵌入式Node构建。

### 2026-09-10 关于页修复部署确认

- root 将1df95f66的前端产物重新嵌入Go二进制，确认两台验收Agent均空闲后原位切换18766至dagents-node-ui-final.exe（PID27716）。原工作目录、环境与数据保留。
- 实际18766关于页390×844暗色截图确认两列状态卡片、单行版本标签与windows-amd64；至此该修复不仅在Vite源页面，也在部署版本通过。浏览器视口已恢复。

### 2026-09-10 Manage 窄屏模型配置

- root在8022实际管理台登录，390×844暗色检查配置/LLM空态与新建LLM弹窗；名称、URL、密钥、模型、可见组及底部取消/保存均在视口内，无裁切重叠。取消空表单，没有保存配置，恢复默认视口并保留管理台标签页。
- 隔离恢复实例18768已由Luna准备；它只用于人为构造未确认投递状态的恢复UI/API验收，不代表真实LLM执行中断的故障注入证明。

### 2026-09-10 默认触发器恢复实页/API验收

- 独立18768临时Node：正式API创建Auto/profile，停机后仅在该实例构造未确认投递，重启保留恢复屏障。此为持久状态夹具，不是真实LLM崩溃模拟。
- root实际浏览器确认Auto设置提示“上次投递需要核对”；触发器桌面及390×844暗色卡片显示禁用、受Auto管理、需要核对；恢复、打开Auto设置、历史入口可达，未暴露通用编辑/启用/删除/立即执行。
- HTTP错误delivery和错误revision分别409，读回确认pending/revision未变。正确CAS恢复200，pending及recovery标志清除且仍禁用；浏览器刷新确认恢复入口消失。
- 浏览器点击“打开Auto设置”后出现“唤醒配置尚未同步”，点击“同步唤醒”，API确认仍只有同一条默认trigger、enabled=true、fire_count=0，未重放旧投递。
- 恢复确认本身使用正式HTTP API，未点击原生window.confirm；不能声称浏览器确认弹窗交互已实测。结束时恢复视口、关闭临时页、验证进程工作目录后停止隔离Node，保留夹具文件。

### 2026-09-10 恢复链接主题与原生确认闭环

- SimplifiedAutoPanel恢复链接增加主题色、hover背景与focus-visible轮廓，保留a链接语义。Luna前端构建通过，root嵌入隔离Node后暗色截图确认默认深蓝下划线已消除。
- root在18768专用夹具点击“确认并丢弃旧投递”，实际浏览器返回原生确认文案，明确不重放、恢复后禁用、回Auto设置同步。getJsDialog返回undefined，随后通过支持的Return按键确认成功；页面显示“已丢弃旧投递”，需要核对/恢复按钮消失，默认trigger仍禁用、触发0次。此项补齐此前未实测的原生确认交互。
- 关闭临时页并核对工作目录后停止隔离PID21272。生产18766未在此批次更换二进制，链接样式待下一次部署。

### 2026-09-10 Agent资源与审批表单窄屏修复

- MCP/Linux标题栏说明允许收缩换行，保存/刷新按钮保持单行。root在5173浅色390×844实际截图复验通过。
- 临时授权input复用横排搜索框flex-basis:180px，放入纵向settings-field后错误形成180px高度；局部覆盖授权表单内filter的flex，保留搜索框规则。root窄屏实际截图确认目录/有效期恢复正常单行高度，未创建授权或改变审批权限。
- Luna前端构建通过，样式修复待下次嵌入式Node部署。记忆区段空态在18766暗色390px检查通过。浏览器视口已恢复。

### 2026-09-10 资源与审批布局部署复验

- root确认两台Agent空闲后，将6d1d880b构建切入18766（dagents-node-resource-ui.exe，PID6928），保留原环境与运行目录。
- 当前嵌入式UI、390×844暗色实际截图确认MCP保存/Linux刷新单行，以及授权目录/有效期输入框正常高度；未修改配置、未创建授权。视口恢复，临时标签已关闭。

### 2026-09-10 条件脚本实际进程验收（false与reject）

- 隔离18768使用当前Node二进制及专用Agent，创建用户trigger ce8ce185-f243-431d-b348-1b335fc575a3，条件exit 1、86400秒间隔。首次禁用时手动触发被跳过；启用后手动触发进入原bash_run审批。
- delivery 21034fde-2e15-4449-bc34-e0d2b65b2f61批准后历史为skipped/condition not satisfied；主会话transcript空、无active/queue/pending_hitl。
- 再次触发delivery 5d285ad8-39db-4b29-a700-40dd6acca5fa并拒绝，同样终止且会话空。当前历史将拒绝也展示为condition not satisfied，不能据此独立证明未执行脚本；该区分及具有可观察副作用的拒绝证据仍需补查。本轮不声明true投递已实测。
- 已验证工作目录后停止隔离PID21920，保留夹具数据；未改18766配置。

### 2026-09-10 条件false实测结论更正：工具未启用被误分类

- root只读查询隔离sessions.db的turn_events/tool.result.recorded，发现delivery 21034fde（最初exit 1批准）与4eb7a9e6（标记脚本批准）的result_content均为 `ERROR: ERROR: tool "bash_run" is not enabled`。脚本未真正执行。
- 因此此前“条件false实际运行通过”结论撤回：实际仅证明审批后结束，没有证明退出码false路径。拒绝后的无文件证据也不能单独成立，须在启用工具后以成功对照重测。
- 已交Luna修复：执行错误必须区别于条件false，拒绝必须区别于false，保持工具开关和既有审批约束；添加禁用bash_run的生产路径回归。该问题未闭环，整体目标保持进行中。

### 2026-09-10 启用工具后的条件脚本有效对照

- 仅通过正式PATCH给隔离Agent启用bash工具组，未改变审批策略。相同标记脚本批准delivery cc227864-1ea4-4bd7-8141-edf9727de816后确实创建文件；SQLite工具结果为[BASH_RESULT]、status=FAILED、exit_code=1、stderr_bytes=0，首次获得实际非零退出码证据。
- 将该标记移为同目录approved-control-evidence.txt保留正对照，再执行相同trigger并拒绝delivery 5b2e9a6e-589f-48c1-88df-fdf08e1b8172；结束后原标记未重建，正对照存在，transcript空、active/queue/pending_hitl均空。拒绝不执行脚本证据成立。
- 拒绝/执行错误历史分类修复仍在开发，尚未部署或宣告通过；隔离实例保留供后续复验。

### 2026-09-10 条件分类修复与最新Node部署

- `55f2103f` 已构建为 `dagents-node-55f2103f.exe`。定向 Go race 覆盖拒绝/条件 false 区分、禁用 `bash_run` 执行错误、调度错误分类及真实 HTTP 拒绝历史，三个相关包通过。
- 替换前通过 18766 hydrate 核对 `agt-9ea36189d2221ea1` 与 `agt-86cd2b08565d2c10`：均无 active turn、排队消息或 pending HITL。未清理数据库、配置或夹具。
- 18766 已切换至新二进制，进程 PID 25660，健康检查与 `/ui/` 均返回 200，trigger 列表可只读访问。运行时仍使用原隔离 Auto UI runtime 目录；监听使用临时 bootstrap 副本恢复原 18766 端口，原 `config.yaml` 未改动。
- 条件脚本结论保持保守：未把此前未启用 `bash_run` 的结果写成真实 false 路径；隔离库中该情况是执行错误。启用工具后的非零退出码与拒绝对照已记录，但本次只部署分类修复，未触发生产 Auto。

### 2026-09-10 隔离Node进程重启黑盒复核

- `go test ./node/cmd/dagents-node -run '^TestProcessRestartRecovery$' -count=1 -timeout=180s` 与对应 `-race` 均通过。测试启动真实 Node 二进制两次，使用临时端口、临时 SQLite/runtime 和 HTTP fake LLM，验证未知工具执行重开恢复及 HITL 重开恢复，模型调用次数与终态事件均有断言。
- 该进程级 fixture 未配置 Auto profile/default trigger 投影，因此不能据此宣称“默认 trigger 真实到期后只执行一次”。默认 trigger 的启动校正和冻结仍由隔离 API race 覆盖；真实到期黑盒留作后续专门验收，不连接 18766。

### 2026-09-10 默认 trigger 进程级到期黑盒

- 扩展 `node/cmd/dagents-node/process_restart_e2e_test.go` 的隔离 fixture：真实 Node 进程启用 triggers，正式创建 `agent_type=auto`，通过 `/v1/agents/{id}/auto-config` 设置合法 1 秒间隔，等待 `auto-default:{id}` 到期并观察 `fire_count=1`、一次 fake LLM 请求、`turn.completed` 及 hydrate 无 active/queue/pending HITL。
- 首次回合完成且 delivery 已结算后，通过正式 auto-config 将隔离 profile 调整为合法 60 秒间隔，保持默认 trigger enabled 并使 `next_fire_at` 位于未来；随后停止并重启同一临时 runtime。重开后读取 trigger 与 hydrate，断言 controller/owner 身份正确、enabled、无 recovery/pending、`fire_count` 和 fake LLM 调用数仍为 1。普通与 race 进程黑盒均通过；未触发 18766。

### 2026-09-10 页面可达性复核

- 只读 HTTP 检查通过：18766 的主 UI、触发器设置、反馈、通用设置、帮助页，以及 8022 的 Manage 根页和 Console 均返回 200。
- 本轮 CUA 内置浏览器返回 `No browser is available`，未取得桌面或 390px 视觉证据；因此不把上述 HTTP 200 记录为布局通过，Node/Manage 逐页视觉矩阵仍待可用浏览器后复验。未修改配置、创建数据或触发 Auto。

### 2026-09-10 默认 trigger 重启边界复核

- 在隔离临时目录上的 `TestNewServerStartupRebuildsOnlyAutoDefaults` 与 `TestNewServerStartupKeepsPendingAutoDefaultFrozen` 以 `go test -race` 重跑通过，覆盖重开后的默认 trigger 校正、待恢复投递冻结及普通 trigger 隔离；未触发 18766 的 Auto。
- 当前 18766 只读 `GET /v1/triggers` 返回 6 条 trigger，其中两个 Agent 默认 trigger 均为 `enabled=false`、无 `pending_delivery_id`，未因本次检查改变配置。该结果只证明当前部署状态，不扩展为真实到期后模型激活证明。

### 2026-09-10 桌面页面复核补充

- root 使用内置浏览器只读复核了 Node 智能体设置（Auto）、Auto 总览、帮助、反馈、智能体列表和触发器页面，以及 Manage 首页、Auto 员工总览和用户反馈页面；页面均可加载，布局、分组、表单和按钮未见明显断裂。
- Node 与 Manage 页面均核对 `document.scrollWidth === document.clientWidth`，未发现横向溢出。
- 本批证据仅覆盖桌面视口；390px 窄屏的逐页完整复核仍未完成，因此不宣称所有 UI 已通过。复核未修改配置、创建数据或触发 Auto。

### 2026-09-10 18766 收尾部署与旧 Goal 夹具核对

- 本轮已确认并保留以下已完成清理提交的范围：`27fe9238` 清理 Goal trigger 专用生产路径，`ba920839` 清理 Goal 元数据，`84927666` 清理旧术语，`fcc20944` 移除前端独立 Goal 会话投影，`0d3796ae` 清理 NavRail 死逻辑。
- 18766 已更新为 `fcc20944` 构建的 Node 二进制。只读检查确认 health、`/ui/`、`/v1/triggers`、Auto 配置接口及两个 Auto 主会话 hydrate 均返回 200；两个 Auto 主会话均无 active turn、排队消息或 pending HITL。本轮未触发模型，未修改配置或业务数据。
- 18766 的 4 条 `controller=goal` 记录（`auto-intent-b0a14dbe596029b1ce063781`、`7305325b-7730-4232-8621-ed741096442b`、`managed-trigger-df6464d7-bcde-440c-8ebe-933798ffb13b`、`managed-trigger-6b1b1365-5f2b-4160-8b42-b526b9ac43f8`）均由本轮临时验收日志中的旧 Goal/cycle 夹具创建，目标为 `goal-session-*`，当前均 `enabled=false`、`recovery_required=true` 且无 pending delivery。新代码已将其隔离并禁用。
- 现有 DELETE API 只允许 `user` controller；对这些退役 `goal` 记录没有授权删除入口。为避免直接改 SQLite 或误伤 Auto 主会话、Todo、经验、手册及普通用户 trigger，本轮未删除实际数据，保留记录并准确记录其隔离状态。
- 本次仍不宣称全页面 390px 窄屏矩阵完成；该覆盖边界保持不变。

### 2026-09-10 18766 条件脚本 false 实际对照

- 在当前 18766 隔离验收 runtime 中，使用临时用户 trigger `4e0c3165-1f7f-4008-bc5d-05b9bfd504f3`，经既有 `bash_run` 审批批准后执行 `exit 1`。历史最终记录为 `skipped / condition not satisfied`，证明脚本返回 false 时不会投递后续 Agent 任务。
- 对照结束后，Agent hydrate 为 `active=null`、`queue=0`、`pending_hitl=null`；临时 trigger 已通过现有 API 删除。未触发额外模型回合，未改变业务数据。
- 本次将 Auto Agent `agt-86cd2b08565d2c10` 的工具组恢复为 `fs`，未修改其 Auto 配置、Todo、经验或手册内容。

### 2026-09-10 退役 Auto 影子与旧维护闭环清理

- `c44f2085` 删除仅被测试引用的 `RiskLLMHost` 独立风险影子 LLM，以及旧 `MaintenanceReconciliation` 闭环实现与专属测试，切断已退役 Auto 风险/维护产品路径。
- 保留 session lifecycle 的通用 reconciliation、dreaming gate、maintenance gate 与 memory consolidation；`turn`、`api`、`session`、`triggers` 相关 Go 包测试通过。
- 清理后工作区保持干净。
