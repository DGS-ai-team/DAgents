# Auto 数字员工实施进度与验收台账

依据：[开发方案](2026-09-08-auto-employee-implementation-plan.md)。开始日期：2026-09-08。状态：实施中，完整目标尚未完成。

## 分阶段状态

| 阶段 | 状态 | 当前证据/下一门槛 |
| --- | --- | --- |
| A 触发器归属与权限 | 编码中 | Luna 负责 owner/controller/revision、原子授权、迁移及回归；主 Agent 审查管理 HTTP 与模型身份边界 |
| B 员工与业务周期 | 现状梳理中 | 已确认 autonomy API/tool 按首个 managed Goal 查找，需替换为显式 current_goal_id；等待契约冻结 |
| C 收尾与调度意图 | 未开始 | 依赖 B；Goal 与 Trigger 当前不同文件，需单事实来源和可恢复投影 |
| D UI | 独立子包编码中 | 先做 Auto 标识与列表分组；总览、详情和新设置依赖 B/C API |
| E 工作区与事件 | 未开始 | 同目录展示不作为并发控制；写租约上线前不开放新并发承诺 |
| F 授权与维护 | 未开始 | 沿用既有 policy；后续扩展岗位授权、版本验证与回滚 |
| G Manage 汇总 | 未开始 | 依赖 Node 汇总契约；先只读且明确数据新鲜度 |

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
