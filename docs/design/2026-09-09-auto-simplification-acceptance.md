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

1. 将新版可执行文件用于本地验收运行环境。
2. 使用已配置 LLM 的 Agent 验证普通聊天、Todo、自主激活、dreaming 与手册操作。
3. Node/Manage 全页桌面与窄屏视觉验收，以及默认 trigger 恢复入口。
4. 验收后打开最新 Node/Manage 界面，并确认提交与工作区状态。

用户已通过 Escape 停止 Computer Use，尚未恢复界面操作。当前不标记整个目标完成。

## 2026-09-09 真实运行验收补充

- 新版 Node 已原位切换到本机 18766，沿用原 runtime 与已配置 mimo-v2.5-pro；Manage 8022 已重启并通过管理员 UI 登录。
- 真实模型普通聊天调用 todo_create、todo_update，经两次原审批生成 completed/revision2 的“验收：67×19=1273”。
- UI 保存职责、60秒默认激活与3轮上限。实际 scheduler 自动触发一次，主会话显示固定激活消息，模型读取新待办并经原审批更新为“验收：71×17=1207”，completed/revision2。验收后频率关闭，默认 trigger fire_count=1、target_session_id 与 Agent 主会话相同。此项不单独证明已触及工具轮次上限。
- 本地 Manage node token 原绑定测试别名，与实际 UUID 不一致；修正绑定后注册、心跳、Auto 摘要上报返回200，WebSocket连接成功。未放宽角色或关闭鉴权。
- 内置浏览器检查发现 Auto 原生控件/待办标题及侧栏入口不协调，修复已提交e37853aa；Manage旧缓存导致总览500，严格读取隔离修复f64bf9e2，10项Python测试及登录后页面空状态复验通过。
- 新发现前端重启重连审批显示延迟、模型修改Todo后面板陈旧，仍在修复。真实dreaming、全页及窄屏视觉验收尚未完成。以上取代前文“真实LLM和新版环境均未开始”的旧状态，不扩大已验收范围。
