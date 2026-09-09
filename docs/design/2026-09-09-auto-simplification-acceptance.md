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

## 尚未完成的产品验收

1. 新版可执行文件已用于本地验收运行环境；后续代码修复仍须重建后复验。
2. 已配置 LLM 的普通聊天、Todo、自主激活和 dreaming 提交/重置已有下文证据；真实手册写入与工具轮次触顶仍待验证。
3. Node/Manage 全页桌面与窄屏视觉验收，以及默认 trigger 恢复入口。
4. 验收后打开最新 Node/Manage 界面，并确认提交与工作区状态。

用户随后已恢复界面操作，并指定使用内置浏览器，授权重启服务与临时管理员登录。当前不存在该操作阻塞，整体产品验收尚未完成。

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
