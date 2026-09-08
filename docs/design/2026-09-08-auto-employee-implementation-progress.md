# Auto 数字员工实施进度与验收台账

依据：[开发方案](2026-09-08-auto-employee-implementation-plan.md)。开始日期：2026-09-08。状态：实施中，完整目标尚未完成。

## 分阶段状态

### 2026-09-09 相对唤醒两轮真实验收

隔离 Node 18766、Agent `agt-9ea36189d2221ea1` 使用已配置真实模型，周期 `6b1b1365-5f2b-4160-8b42-b526b9ac43f8` 已完成。Run `d8ae1c75-7ae1-4ade-b7de-b040de7d910d` 记录 901，消耗 6285 tokens；Run `6a99f3c7-b805-490b-a504-35bb6c5e324c` 记录 767，消耗 8638 tokens。合计 14923 tokens。两次 checkpoint 均经现有显式审批，未修改工具权限。

第一轮相对 90 秒决策保存为 2026-09-08T17:35:31.3358508Z，第二轮实际于 17:35:34.4738902Z 开始，符合 5 秒轮询。完成后实际 trigger `auto-intent-b0a14dbe596029b1ce063781` 为 enabled=false、next_fire_at=null，Auto 摘要 state=standby、next_at=null。

中途读到旧 Goal.next_wake_at 时曾怀疑排程丢失；后续真实 Run 记录证实调度成功，未经证明的生命周期兜底已撤销。剩余呈现缺口：仅 decision.summary 有内容时 last_summary 为空，终态 Goal 保留旧 next_wake_at；不能据此声称所有 UI 状态已验收。

### 2026-09-09 补充独立验收（未通过项）

- 隔离 Node 18766 的周期 `df6464d7-bcde-440c-8ebe-933798ffb13b` 经实际 API 读回为 `paused/user_paused`，Runs=1，消耗 2609 tokens，summary.next_at=null。两轮真实验收尚未成功，不把暂停或定向单测计为完成。
- 常驻配置 UI 仍需修复：旧自由文本压过结构化选择、周几清空回退旧值、周期时长静默截断、保存周期覆盖未保存的结构化岗位草稿。新增控件尚未验收通过。
- 工作区 Terminal 租约需由实际进程退出驱动；取消失败、启动与关闭竞争、启动前 Wait 均需明确测试。原使用已取消 context 的竞争测试不能证明写互斥。
- 相对唤醒协议需补真实 callback 测试：同一可信 now、绝对/相对互斥、间隔与过期边界、非法决策报错。既有 Decision/Wake/Recurring 定向 Go 测试通过，仅证明既有覆盖范围。
- 每日维护准备文档须修正跨存储事务假设，并覆盖未触发压缩的新增已完成对话；异步候选入队不等于维护完成。

| 阶段 | 状态 | 当前证据/下一门槛 |
| --- | --- | --- |
| A 触发器归属与权限 | 基础与 API 已提交，待最终审计 | 7b93043a、2c5bf738；owner/controller 原子授权、迁移与 HTTP 集成已有测试；最终全入口权限审计仍保留 |
| B 员工与业务周期 | 已集成 | 显式 current_goal_id、多周期、历史与累计用量；2c5bf738 API 全包独立通过；不能据此代替最终产品验收 |
| C 收尾与调度意图 | 定时路径已通过真实两轮验收 | 488ce69a、2c5bf738；相对唤醒、投影、离线 coalesce 与重启去重已有证据；跨两个常驻业务周期持续运行仍待验收 |
| D UI | 部分完成 | 类型标识、分组、总览/详情已实现；设置页结构化控件收尾中；12ec4f7c 修复 decision-only 摘要，最终全页与窄屏回归仍待完成 |
| E 工作区与事件 | 写锁已提交，事件探针验收中 | 5bc3d1f、3eda83db；工具包及定向 race 通过；events 包尚未接注册配置、ScheduleIntent/projector、UI 健康状态 |
| F 授权与维护 | 预算底座已提交，维护事务验收中 | 4d8a39f7；每日维护调度、稳定日志输入、真实提取、版本验证/回滚、受限岗位授权及风险影子评估尚未完成 |
| G Manage 汇总 | 前后端已实现，实际联调未通过 | 9f37fd13、c13d83b5、2c5bf738；实际 Node UUID 与测试凭据需一致；配置保存不等于注册/上报成功 |

本表为当前状态；下方按时间保留的初轮问题和“未开始”叙述属于历史记录，不能覆盖本表及具体最新证据。

### 下一步集成交付门槛

1. 事件唤醒：管理员配置私有 source → 注册/归属校验 → 模型 next_action=event → 持久意图投影 → 文件变化只触发一次 Run → 完成后撤销订阅。扫描失败可见且退避，无变化不调用模型。
2. 每日维护：启用/时间/额度设置 → 已完成日志增量 → 无新增跳过 → 同 Agent 执行槽与预算预留 → 真实候选提取 → 记忆/游标一致提交 → 消耗对账 → 历史、差异、验证与回滚。空候选批次也应推进成功处理游标。
3. 审批连续性：明确岗位授权作用域、失效与撤销；LLM 风险识别仅影子建议，不能覆盖显式拒绝。两轮 checkpoint 经人工确认通过，不证明无需审批的自主性体验已经交付。
4. 真实验收继续覆盖对话改计划读回、跨常驻周期与一次每日维护、重启/故障恢复、Node→Manage 实际上报、完整 Node/Manage 视觉检查。所有测试数据保持隔离，最终清理验收排程并打开最新界面。

## 初始核对

- 开始实施时 Git 工作区干净，方案提交为 a8081baf。
- 当前触发器工具已有目标 Agent 过滤，但没有独立 owner 与存储原子授权。
- Server.Handler 当前为 access log 与 onboarding gate，不存在可直接复用的 Node 多用户认证。管理 HTTP 属于现有本地可信宿主边界，不接受客户端自报 Agent ID 作为身份。
- Goal 当前使用文件快照持久化；lookupGoalSession 基于专用 Session ID，历史查询必须保留，不能一律改成只查当前周期。
- 基础回归命令：`go test ./node/internal/goals ./node/internal/triggers ./node/internal/tools ./node/internal/api -timeout 120s`，2026-09-08 完成通过。此结果仅为初始基线，不证明正在修改的实现通过验收。

## 验收记录要求

每阶段记录实际提交、测试命令与结果、人工审查结论及未解决限制。最终审计逐项覆盖方案 T01–T10、真实 LLM 两条链、跨周期/每日维护、重启故障注入、Node/Manage 视觉验收，不用单元测试替代全部产品验收。未完成项保持可见。


## 第一轮实施审查

- UI 已在隔离前端 18768 验证显式 Auto 类型分组、普通筛选空状态、目录组折叠及不改变当前聊天；桌面 1280 和移动 390 像素截图检查通过初轮布局。搜索控件已改为独占一行，类型/分组放下一行，避免半行留空。后续仍需总览/详情与最终全页验收。
- 独立运行 `npm test --prefix node/webui/frontend -- --run src/components/NavRail.test.js src/utils/agentGrouping.test.js`，3 项通过；偏好 Node 隔离与更多交互测试仍在补充，不能据此宣称 D 完成。
- A 首轮仅元数据，退回补原子权限。随后发现恢复和 fire 存在检查后解锁再操作的问题，继续要求在实际 claim/恢复同锁校验，并增加真实竞争窗口测试。
- B 首轮新增 Profile/Cycle 存储，审查要求修复 map 遍历导致幂等随机失败、当前周期引用被任意修改、累计用量写盘回滚与越界计账问题。实际消耗即使超过预算也必须完整记账，再阻断后续运行；不能因预算耗尽拒绝记账。

## D 独立 UI 子包验收（2026-09-08）

AutoBadge 已用于侧栏、设置列表及 Auto 聊天提示；侧栏和设置列表支持类型分组、搜索与筛选，侧栏支持目录分组/折叠；私有工作目录按 Agent 独立分组。偏好以 bootstrap 的 Node ID 为作用域，包含实际 A→B→A 挂载恢复测试。

主 Agent 独立执行全量前端测试：62 个文件、329 项通过；ESLint 和 Vite build 通过。截图及交互检查覆盖隔离 Node 的桌面/390 像素列表、Auto 类型筛选、目录折叠及设置列表。当前子包可提交；Auto 总览、员工详情和新周期设置仍未实现，不代表 D 阶段完成。

## 后端验收推进

主 Agent 独立运行 `go test -race ./node/internal/triggers -run TestAuthorized -count=1 -timeout 120s` 通过。新增用例覆盖 owner 隔离、历史拷贝、controller 限制及 fire 初始授权后版本发生变化时不投递；这只证明已测试的授权路径，A 尚待管理员 CRUD、迁移/启动失败与工具完整集成验收。

后续依赖保持不变：B 存储层通过审查后接 autonomy API/current cycle/累计预算；C 再接收尾决策与调度意图；D 总览和详情必须使用真实 API。当前正式运行服务尚未加载本轮后端改动。

## API 集成复审（2026-09-08）

主 Agent 独立执行 `go test ./node/internal/api ./node/internal/goals ./node/internal/triggers ./node/internal/tools -timeout 120s`：goals、triggers、tools 通过，api 失败；不能将窄范围通过视为后端闭环。重启后的 Goal 唤醒未调用模型仍需定位并修复，尚无证据可将其排除为无关问题。

代码复审退回 B API：空历史与超大页码的乘法溢出、缺岗位时隐式创建配置、幂等请求未区分启用意图，以及 PUT 在拒绝请求前已保存 Profile 的部分写入问题。要求增加失败响应不改变配置、暂停/终态重放不恢复运行、资源部署失败可幂等重试的测试。A 的结构化授权审计仍缺失。上述项闭环前不提交后端阶段完成结论；OpenAPI 正在按最终接口同步。

后续独立验证：`go test -race ./node/internal/triggers -run 'TestAuthorized|TestLegacy|TestFuture|TestValidate' -count=1 -timeout 120s` 通过；冷启动测试改为启动前持久化 Agent、重启时正常打开存储后，`TestGoalColdRestartKeepsDedicatedRuntime|TestFutureSchemaStartupRejected|TestStartupValidation` 通过。这是针对性恢复验证，仍待全量 API 重新通过。OpenAPI 路由与契约两项 CI 检查通过。

部署资源复审还发现随机 Trigger ID 配合失败后尽力删除不能证明幂等，要求稳定身份和实际失败注入验证。D 的 Auto 总览聚合接口已开始独立实现；必须返回真实岗位、周期和调度状态，后续前端不自行推测状态。

继续复审：稳定 Trigger ID 已加入，但绑定完整后的投影失败重试仍会被跳过，部分错误仍丢失 Goal ID；已要求持久化部署状态并逐故障点测试。PUT 的尽力回滚仍可能改变 revision 或留下不同步的 Profile，要求同锁单次持久化岗位与周期配置。主 Agent 再次独立运行 goals 全包 race 测试通过；API 全量尝试被审计代码编辑中的编译错误阻断，不记录为通过。

Auto 总览首稿退回修正岗位停用优先级、仅使用明确 CurrentGoalID、排除归档 Agent、终态成果与员工状态分离、有效调度来源及分页解析；尚未进入视觉验收。

本轮完整 API 独立回归 `go test ./node/internal/api -count=1 -timeout 120s` 通过（27.209 秒）。随后 Node 全量回归遇到正在进行的事务接线编译中间态，不能记录全量通过。新的 UpdateProfileAndCycle 已落地同锁单快照基础结构，仍需补齐终态不可复活、预算/期限校验及写盘失败双实体不变的故障测试。总览接口继续修正员工状态与任务终态分离，并准备接入真实前端。

主 Agent 独立复验整个 `triggers` 包 race 通过（1.807 秒），总览/触发器授权/启动错误相关 API 测试通过。工具创建仍需接入 CreateAuthorized 以统一审计。事务故障测试审查发现其删除旧磁盘数据后断言重开为空，不能证明旧数据保留；要求保留旧文件再注入失败、还原后核对两个实体及分别过期的双版本。当前不以此测试支持持久化恢复完成声明。

## 触发器基础子包提交

`7b93043a` 提交触发器存储/调度授权、版本检查、迁移、审计及工具可信身份接线，共 11 个文件；API 管理入口和启动校验尚在后续未提交集成包，不把该提交称为 A 整阶段完成。

提交前主 Agent 独立执行 `go test ./node/... -timeout 120s` 全部通过（API 28.947 秒）；完整 Trigger race 已通过。事务故障测试已改为保留原文件、注入目标写失败后恢复并核对两实体。B 仍待持久化部署状态与阶段失败重试；C 开始纯收尾决策模型与校验，D 开始总览前端真实接口接入。工作区仍有后续实施中的变更。

## 总览视觉复验与 C 基础提交

隔离验收 Node 18766 已更新为本轮编译版本，原运行目录先备份，正式 Node 未替换。18768 总览能读真实 Agent；发现旧 Goal 的 Profile 仅懒迁移导致总览误报未配置，已要求启动迁移先于调度。桌面统计按钮配色修复已截图确认；390 像素实测 document scrollWidth=666、clientWidth=382，main 左侧仍占约 220 像素导航，移动验收失败并退回修复，不能宣称 D 完成。

`63355ea8` 提交纯 FinalDecision 校验模型及测试，无调度副作用；主 Agent 独立运行 `go test ./node/internal/goals -run 'Decision|TestUpdateProfileAndCycle' -count=1 -timeout 120s` 通过。C 的持久化意图与 finalize 集成正在开发，当前提交不代表自动续跑已实现。

移动总览再次实测 clientWidth/scrollWidth 均为 390，完整行卡、操作及分页可见，统计点击能请求对应筛选。主 Agent 独立前端全量回归 63 文件 / 333 项通过；顶部总数与过滤后条数仍需分离。启动迁移复审发现曾错误复用仅 Auto 的集合校验普通 Trigger，已拆为所有 Agent 与 Auto 两个集合，专项真实启动测试正在补充。C finalize 首稿因整实体覆盖、未计账和过早标为 projected 退回重做，尚未接入运行链。

启动专项首次因真实产品配置 seed 导致 onboarding 403；修正持久化 node settings fixture 后，主 Agent 独立 `TestStartupMigratesAutoBeforeOverview|TestAutonomyCyclesHTTPProvisionDiskFailureRetryKeepsResources` 通过（0.737 秒）。测试覆盖启动即迁移、usage=100、普通 trigger 有效及 MarkReady 磁盘失败重试资源不重复；不扩大为完整故障矩阵通过。总览 counts.total 已与状态筛选后分页总数分离，终态员工状态表驱动通过，归属错误/停用不倒计时测试继续补齐。

`585751fa` 提交总览前端子包（5 文件）：导航、真实聚合请求、筛选/分页、移动抽屉及行卡、错误与乱序响应处理。主 Agent 复验 TestAutoOverview 与前端 4 项交互测试通过；Node 全包再次通过（API 28.162 秒）。后端聚合接口仍在未提交集成包，不能只部署此 UI 提交就宣称完整功能上线。D 下一步为员工工作页与历史，C 继续修复旧配置收尾不得完成新目标、无决策仍记账及旧意图撤销。

`8d531f5d` 提交 Goal 存储基础：AutoProfile/周期/累计用量、配置事务、部署状态以及独立 FinalizeRun/调度意图存储。主 Agent 独立 Goal 全包 race 通过（1.676 秒），包括旧 revision 无效决策不暂停新 Goal、旧 none 不撤销更新 generation 的回归。该 finalize 尚未接 ObserveTurn/FinishRun，投影器与投递校验进入下一工作包，不将此提交表述为 C 全部完成。工作页与总览统一 summary 正在补两入口一致性测试。

工作页真实浏览器可展开历史完成周期并显示 2026-09-08 16:26:58 的 Run；桌面垂直居中和无周期时缺聊天/设置入口已退回修复。actions 首稿复用了拒绝 busy/terminal 的配置事务，无法满足运行中停止及终态岗位停用，要求专用动作事务、同快照撤销意图和取消当前 Turn，并禁止 enable_auto 隐式恢复手动暂停。

## 收尾链路接线审查

主 Agent 独立执行 `go test ./node/internal/goals ./node/internal/api ./node/internal/triggers -count=1 -timeout 120s` 通过（API 29.027 秒）。这证明现有回归通过，不证明 C 已接通。源码确认如下缺口，已分别交 Luna 修复：

- `ApplyAutoAction` 仍未原子撤销意图，resume 未覆盖 Profile 总额度，API pause 未取消 Turn；动作专用失败注入测试缺失。
- `AutoIntentProjector` 已移除吞掉 ConfirmProjected 错误的分支，但 projected 快捷返回及 revoked 的读后写仍需并发版本证明；必须补真实投影测试和投递前回查。
- `AutoWorkView.test.js` 原测试只验证重试按钮存在，未点击重试或分页；路由测试没有迟到请求，需补真实交互并卸载组件。

下一步 C 接线必须同时处理以下入口，不能仅新增独立 FinalizeRun：

1. `tools/goal_tool.go` 与 `api/server_wiring.go`：收尾决策作为当前 Run 的候选保存；目前 checkpoint 回调直接 UpdateNextFireAt，会提前发布唤醒，需在新协议中移除该副作用。
2. `goals/models.go` 与 StartRun：保存启动时 Profile/配置版本及调度 generation，避免收尾时使用最新版本冒充原始授权。内部进度变化与外部配置变化要区分，不能让正常 checkpoint 自动使自己的收尾失效。
3. `goals/run_lifecycle.go`：完成路径将 Run 状态、实际用量、Goal 变化、决策与意图在同一次保存提交；失败、取消、未知用量不能被统一写成 completed；晚到结果仍需计账但不得恢复已停用岗位。
4. `api/server.go`：实际 ClaimDelivery/MarkFired 当前使用 Goal.TriggerID；新 hash 投影必须与实际投递 ID 一致并撤销旧 interval 排程，避免新旧两套调度同时存在。
5. 重启恢复：先恢复事实与 pending 投影，再开放调度；每次投递回查 owner、当前周期、开关、generation、预算与执行槽。重复回调、投影失败和停止竞争需用真实入口集成测试证明。

当前未替换正式 Node，不把编译成功或独立存储测试作为自主循环上线证据。

## 员工工作页提交与投影复验

`02a8860c` 提交员工工作页、历史分页/重试和总览跳转。主 Agent 独立验证 5 项工作页交互测试，随后前端全量 64 文件 / 339 项通过；API/OpenAPI 契约检查通过。真实浏览器桌面复验导航、主题链接与顶部入口，390 像素 clientWidth/scrollWidth 均为 390，历史周期展开显示真实 Run 时间 2026-09-08 16:26:58。隔离 Node 仍为旧摘要二进制，页面“未配置”不作为新版摘要状态的验收证据。总览按钮“打开聊天”与工作页跳转不一致已交 Luna 修正为独立工作页/对话入口。

投影专项 `go test ./node/internal/api -run AutoIntentProjector -count=1 -timeout 120s` 独立通过（0.179 秒），证明重复未消费投影、旧 generation 撤销拒绝、停用后投影拒绝及撤销返回 disabled。仍缺写失败恢复和真实两代投递证明。新 LastFiredAt 消费判断会与保留跨 generation 历史冲突，第二代可能被永久判为已消费，已退回要求按代次追踪，不能仅验证第一次执行。

C 继续接 Run 启动版本快照、checkpoint 候选与真实终态 finalize；D 继续岗位设置、新周期和明确启停动作。E/F/G 范围未缩减，仍待后续实施和持续运行验收。

## 岗位设置分区保存验收

前端新增岗位职责/边界、模式/时区/安排，岗位与周期分别保存，启停使用明确动作；新周期失败重试保留幂等键，成功后读取完整 autonomy。Agent 切换重建面板并拦截迟到加载，保存一个分区保留另一区未保存草稿。主 Agent 独立前端全量回归 64 文件 / 344 项通过，含面板 8 项测试。

在隔离 18768 页面真实保存验收 Agent 的岗位职责、边界和 Asia/Shanghai 时区，点击刷新后仍读回同值，Auto 保持停用。桌面一项一行，390 像素 clientWidth/scrollWidth 均为 390。该检查使用既有隔离后端，仅证明岗位保存和布局；最新双版本 CAS、周期启停及新收尾链路仍待更新隔离二进制后集成验收，不扩大为 D 全部完成。

投影 Store 全包 race 独立通过（1.835 秒）。后端继续修复投影后的 Profile 版本投递校验、原子撤销和 HTTP 配置双版本冲突测试；收尾状态机改由独立 Luna 子任务集中重构，避免新旧状态分支互相覆盖。

## 2026-09-09 收尾提交与集成复验

`668975bc` 提交 managed Trigger generation 投影与原子撤销基础；`8635a3fd` 提交真实 ObserveTurn 共用的持久化收尾路径、配置版本隔离及迟到周期用量记账。主 Agent 独立 goals 全包 race 通过（1.881 秒），包含零用量暂停、审批结束清除旧原因、失败决策不续排及旧周期迟到结算。

API 两轮唤醒、投影和启动恢复专项通过（0.882 秒）；随后 API 全包回归失败，正在定位，不能据专项通过宣布 C 完成。Trigger 全包通过（0.698 秒）。正式 Node 未更新，真实 LLM 新协议仍待验收。

Manage 新摘要接口和只读页面处于审查阶段，发现重复快照刷新 received_at、同时间内容冲突及时间校验边界，已交 Luna 修正；尚未与 Node 上报形成端到端闭环。常驻岗位下一周期、事件探针、每日维护及全量产品验收继续保留在范围内。

Manage 独立扩展回归：摘要、Node 注册身份、反馈、Console 登录与基础鉴权共 21 项测试通过（6.816 秒）。页面审查发现固定 200 条截断、原始英文状态及移动端宽表格，已退回补分页/Node 筛选/状态映射/窄屏布局。服务端快照新鲜度与 Node reporter 接缝继续开发，当前不宣称 G 完成。

独立复验：TestGoalScheduledWakeRunsTwoTurns 连续 10 次通过（4.140 秒）。仍要求实际调度以 durable intent DueAt 为唯一到期事实，避免仅通过测试取 Goal/Trigger 较晚时间掩盖双时间源差异；resume 生成新意图继续开发。

Manage 浏览器验收：隔离 8021 服务使用新静态页面但旧后端，Auto 页面显示明确错误区域（接口404），不能视为数据验收。390 像素 document clientWidth/scrollWidth 均390；筛选input/select样式仍未统一，错误缺中文上下文，已退回。恢复浏览器视口。

日历纯函数尚未接受：发现 weekly 前缀可省略、夏令时归一化依赖 Go Date 的偏移选择、缺回拨及半小时转换测试，已交 Luna 修正；不将模块编译通过视为 recurring 控制器完成。

最新独立回归：API 全包通过（28.340 秒）；goals 与 triggers 全包 race 分别通过（1.866 / 1.570 秒）。恢复动作的过去 due_at、用途限制和代次溢出仍在补专项，不据此提交未审查边界。日历 DST 修正版已读代码，待补 Windows tzdata 与跨周断言后接收。

Node reporter 当前仅独立HTTP客户端，尚未接 registrar 生命周期或全Agent provider；审查要求真实body白名单断言、多员工上报与总耗时约束。准备另起8022隔离Manage测试实例供数据页面验收，8020/8021服务保持运行。

Manage 提交前复查：明确 Python313 路径运行 21 项回归通过（6.363秒），Console lint通过；当前 shell 的 python 默认解析到无FastAPI的Anaconda，首次导入失败属运行环境，已使用已配置Python313重跑。OpenAPI尚缺409/server_time/stale/age_seconds，Record allOf与closed base冲突，已交Luna同步契约；提交暂缓直到修正。

8022 数据视觉验收已完成分页交互：24条合成摘要，第2页4条，移动4卡，clientWidth/scrollWidth均382（390视口含滚动条），已恢复视口；仍不作为真实Node reporter集成证据。recurring初版发现ProfileRevision/幂等次序/expiry与Run fencing不足，继续修正并新增专项。

`d4ed426e` 提交Trigger pre-tick reconcile与幂等撤销，独立Trigger race通过1.809秒。`16ee7282` 提交recurring Store原子创建及测试，独立Recurring race通过1.432秒：新Goal/intent/profile同快照、重试/并发/重启、授权版本/预算/未知Run检查、写失败回滚。该提交尚未接实际授权配置和scheduler controller，不等于常驻员工可用；已继续交Luna接真实API/controller。

恢复动作专项独立race通过1.545秒，但审查发现event测试错误map键及忽略GetScheduleIntent错误，已退回改为真实记录与完整快照断言。尚未通过验收的event恢复/API错误映射与空session投影继续修正。

`9f37fd13` 提交Manage独立摘要存储、Node身份限定提交、管理员分页总览、服务端120秒新鲜度及Console页面；主Agent独立23项Manage相关回归通过8.704秒，含真实HTTP121秒快照过期；此前分页与窄屏浏览器证据保留。Node真实provider尚未完成，本提交不是G端到端完成。

`33c28110` 提交resume单次预检查/新代次/事件未支持明确错误/溢出拒绝/写失败完整回滚；主Agent恢复专项race通过1.479秒。周期controller及真实provider继续集成；E工作区并发实现接缝交Luna梳理，需覆盖后台进程租约，不得仅工具函数返回就释放写入权。

新版隔离运行验收：构建最新Node前端和dagents-node-20260909.exe，备份隔离.runtime后启动PID26852监听18766。浏览器使用既有agt-9ea36189d2221ea1/mimo-v2.5-pro配置发送无工具算术对话，真实返回“新版对话验收：893”，UI显示本轮2707tokens（输入2628/输出79），Auto保持未启用。此证据仅证明新版对话与真实LLM配置，自动唤醒和recurring仍待独立链路验收。

workspacecoord初版独立race通过2.513秒，审查补已取消ctx不得grant与symlink/..归一化边界；工具write/search_replace接入后仍需Node共享实例及真实双Registry并发验证，bash/terminal后台租约尚未完成。

真实两轮自主验收尚未通过：Goal df6464d7-bcde-440c-8ebe-933798ffb13b / Run ad785345-9d58-4b83-9bfa-b8ed6fa2ca56 / Turn turn-04902d6819e0c5b5 在goal_checkpoint等待策略审批。hydrate证实模型decision.next_wake_at为2026-09-09T12:02Z，实际当前约2026-09-08T17:07Z，且缺expected_progress；没有人工代写checkpoint或宣称自动成功。root通过pause_goal取消等待，保留历史，安排修工具schema与可信时间/运行上下文。既有审批策略未静默修改。

E定向独立race `Workspace|BashProcessRetainsWorkspaceLeaseUntilExit|CancelAllSessionJobs` 通过4.556秒；此前全tools race在Windows截图库checkptr崩溃，单列宿主/依赖限制，不把定向结果扩大为全包通过。
