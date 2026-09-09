# Auto 事件探针首包设计

日期：2026-09-09。状态：探针、事件源 API/UI、健康状态、scheduler 投影与 Auto Goal 唤醒链路已实现；真实配置 LLM 下的端到端运行仍需部署验证。

当前实现状态

`node/internal/events` 已提供只读目录探针、有限文件/字节/目录枚举扫描、摘要去重、持久 cursor/pending event、按 source 的可取消投递 claim、配置 owner/revision CAS，以及指数退避。Node API 已提供 Auto Agent 作用域内 source 的注册、列表、CAS 删除；列表返回 probe state 的 baseline、错误、失败次数和下次重试时间，前端事件源面板展示这些健康信息并阻止旧 Agent 响应回写。`auto_event_controller.go` 已把 probe 接入现有 scheduler：事件命中经过 `AutoIntentProjector` 投影到受限 managed trigger，再复用现有 Goal wake/managed fire 入口。`event_checkpoint_api_integration_test.go` 已覆盖真实 Server 中 checkpoint → event intent → probe 变化 → 第二次 managed Run，并验证无变化不重复运行。pending event 在重启后保持不变，delivery 或状态持久化失败会保留可重试状态并返回联合错误；扫描和投递受 context/timeout 约束。

## 现状结论

当前 `node/internal/triggers` 仍只支持 `interval_seconds`、`fire_at` 和受限 calendar `schedule` 的普通时间触发；`ConditionCmd` 明确记录 skipped，任何 command 都不执行。事件决策模型通过已注册 source、`goals.FinalDecision.Event` / `ScheduleIntent.EventFilter` 和 Auto projector 的受限分支进入事件触发链，未注册 source 或不匹配的 owner/generation 仍返回稳定错误。

现有 trigger fire 会先做 enabled、shell gate、pending delivery 检查，再解析固定 session/目标 Agent，使用持久 delivery claim 和 `FireRecord`。这条路径适合作为事件命中后的唯一投递入口；探针不应另建调度器、直接发消息或绕过 Goal-owned `managedFire`。`ScheduleIntent` 仍是业务意图的单一事实来源，trigger 只是投影。

## 首包范围

首包只接入明确注册的、只读的事件探针 provider。每个 source 有稳定 `source_id`、owner Node、target Agent 约束、超时、轮询间隔、退避上限和不含凭据的配置摘要。provider 返回有界事件记录：事件 ID（若源提供）、发生时间、source ID、目标资源 canonical identity、有限数据摘要和 opaque payload 引用；不返回任意 shell、脚本、URL 命令或可执行表达式。

探针运行在现有 Node scheduler 的受控 tick 中。命中后构造/核对对应 `ScheduleIntent`，再调用已有 managed trigger fire/Goal wake 入口。事件本身不改变 `DueAt`；`NextEvent` intent 在 source 未注册、owner/generation 不匹配或 projector 校验失败时保持 pending 并记录可见错误。

## 不唤醒规则

探针每次读取结果后先 canonicalize 目标目录/资源身份并计算稳定数据摘要。若目标 canonical identity、摘要、版本/ETag 和事件游标均未改变，不创建 Run、不 fire trigger、不唤醒 LLM。摘要必须有大小和字段数上限，过滤密钥、内容和凭据；只有摘要改变或明确的新事件 ID 才进入命中流程。事件 payload 只作为受限上下文传给 Goal，不能影响 owner、session、intent generation 或工具权限。

去重游标持久化在 Node runtime store，至少包括 source ID、owner Agent ID、probe config revision、cursor/last event ID、last canonical target、last summary digest、last successful poll time、failure count、next retry time 和 state。提交游标与“已投递”必须有明确顺序：先以 source+event identity+intent generation 做幂等 claim，成功投递后再推进游标；重启时重复读取只能命中已有 claim，不得重复唤醒。无法证明投递成功时游标保持旧值并进入退避/人工可见状态。

## 所有权与安全边界

每个 probe 绑定 `(node_id, owner_agent_id, controller, controller_id, source_id)`，并保存 intent ID、generation、fingerprint。收到事件时必须核对当前 profile、Goal、ScheduleIntent 的 Agent/owner/controller/generation；旧 intent、他人 Agent、已撤销或 profile revision 不匹配都记录 skipped，不投递。固定 session 必须是现有 Goal session，不能由事件 payload 指定新 session；跨 Node source 只可通过显式 provider identity 和授权协议接入，首包不提供全局锁或全局 owner 声明。

任意 shell、`condition.cmd`、事件 filter 中的命令字段和未注册 source 一律禁用。未知 condition 不降级成可执行 manual；返回稳定错误分类并在 Auto 总览显示。事件 source 的过滤器沿用 `EventSpec` 的 equals-only、有界深度/大小验证；source provider 还要做响应大小、字段、事件数量和请求超时限制。

## 错误、退避和可见性

错误分为 `event_source_not_registered`、`event_source_disabled`、`event_source_timeout`、`event_source_unavailable`、`event_cursor_conflict`、`event_payload_invalid`、`event_owner_mismatch`、`event_delivery_pending` 和 `event_projection_unsupported`。每次失败不改变有效 intent generation；持久 failure count 与 next retry time 使用指数退避并设上限，抖动由 Node 随机源加入。连续失败、游标不确定或 source 配置变更时，Auto 总览显示 source、目标 Agent、最近错误、下次重试时间和“未唤醒”原因；正常无变化保持安静，不生成聊天消息。

探针无法确认远端游标或 delivery 状态时标为 unknown，不猜测成功；Node 重启保留 pending claim，交给幂等 Delivery 重试，不盲删 pending。只有明确的人工恢复流程完成身份核对后，才可清理 claim 并允许重试。探针停止、Node 重启和 provider 取消都必须释放 worker 资源，不遗留后台 shell。跨 Node 只报告 provider 的远端状态，不把本地 dedupe cursor 当作远端事实。

## 已实现的边界与后续验证

1. Node 级受控 probe 已由 scheduler 驱动；provider 不能直接提交 session 消息，命中必须经过 intent/projector 与 managed fire。
2. source registration、cursor、dedupe claim、failure/backoff 已接入持久状态，继续采用单 key CAS 和完整 rollback；事件状态不塞入 `triggers.Definition` 的 interval condition。
3. 仍需在真实配置的 LLM 与实际部署环境中验证完整运行观测、配置迁移和长期退避行为；测试使用受控 mock LLM，不宣称真实供应商调用已验收。
4. 增加 `TriggerStore`/fire adapter：命中后复用 `ClaimDelivery`、`FireRecord` 和现有 `managedFire`，核对 `ManagedGoalID`、OwnerAgentID、Controller、intent ID/generation/fingerprint。
5. 已实现 `NextEvent` projector 的“已注册 source + fixed Goal session + generation CAS”受限分支；未满足条件返回稳定 source-specific error，绝不 fallback 到 interval 或 shell。
6. 增加测试：无变化不 fire；新事件只 fire 一次；重启/重复 poll 去重；cursor conflict 不推进；owner/generation/session 不匹配不投递；失败退避可见；未知 shell/unknown source 永久禁用；source timeout/cancel 不泄漏 worker；Node 间不共享本地 cursor 假设。

普通 trigger 的条件语义保持不变；任意 webhook 或 shell 不是事件 source，也不宣称跨 Node 的 exactly-once。所有事件唤醒仍受 Goal 预算、profile enabled、workspace coordination 和现有 policy 约束。
