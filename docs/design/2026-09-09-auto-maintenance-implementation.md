# Auto 每日维护技术接缝（阶段 F）

日期：2026-09-09。依据：[实施方案 §5.3](2026-09-08-auto-employee-implementation-plan.md)。当前分层实施，生产每日调度尚未验收完成。

## 输入、记忆与用量

维护输入来自 SQLite 已完成 Turn 快照，覆盖未发生压缩的对话。完成事件和快照同一事务提交；读取失败不跳过 cursor，无新增输入不调用模型。MaintenanceRunner.RunOnce 处理最早一段未处理快照。

候选通过 Agent 私有 LocalService 写入，记忆、operation 和 cursor 同一 SQLite 事务。Goal Store receipt 保存预留、候选输出及实际用量。两种存储通过 operation ID 恢复，不声称跨存储原子事务；恢复先处理待结算结果，再读取新增输入。未知用量阻断新预留，不当作零消耗。维护累计计入 Agent 总额度，换日期和业务周期不清零。

## 每日日程与 occurrence

AutoProfile 增加 maintenance_enabled、maintenance_schedule、maintenance_revision、maintenance_epoch_at。日程使用严格 daily HH:MM 与 IANA 时区。

- 维护版本由存储层维护，普通岗位描述或预算修改不改变它；维护配置变化及重新启用更新生效起点。
- 不补跑启用前时刻；离线错过多个日期最多考虑最近一次，不形成补跑队列。
- DST 缺失时刻顺延，重复当地时刻按日期与维护版本去重。
- ClaimMaintenance 原子保存 occurrence；重复 tick 不重新领取，也不把正在运行的 pending 当作崩溃。
- OpenStore 记录当时已有 pending 的 key，新 claim 不受启动恢复标记影响。
- 同 Agent 的 pending 或 recovery_required 均阻止新的日期领取，不能用新日期绕过未确认结果。
- FinishMaintenance 更新既有 occurrence 并保留历史。停用后仍可读历史，最近记录采用确定性排序。

occurrence 负责每日运行协调，receipt 负责模型调用及用量结算；controller 必须关联两者，不能将 occurrence 完成直接等同于候选已经提交。

### 显式恢复契约

一个 occurrence 可处理多段业务输入，因此关联不能是单个 parent ID。每个每日 memory receipt 保存 `occurrence_local_date` 与 `occurrence_schedule_revision`，Agent ID 沿用 receipt 的已有归属；手册 child 继承 parent 的关联。通过这一组合查询该日全部执行，手动执行没有每日归属，不能在恢复时按时间推测或补绑。

每日专用预留方法在 Goals 的同一个锁与文件保存中验证 occurrence 为 pending、没有启动恢复标记、当前维护仍启用且配置版本一致，然后创建带关联的 reservation。API 使用绑定 occurrence 的 Usage 适配器调用该方法，记忆 runner 仍使用同一套提取与提交逻辑。归属与预留不能分两次保存，避免崩溃产生无法归属的执行；重复调用只接受原身份，拒绝将旧手动执行或另一日期执行改绑。

恢复入口读取该 occurrence 的全部执行状态，不能只检查最后一个 child。prepared 可以复用原预留；running 或未知用量必须先对账，不能把状态直接重置为 prepared。对账需记录依据、实际用量及文件历史；缺乏证据时保持待处理。用户可终止后续整理，但已发生的费用与文件修改保留，未知费用不以零代替。重复恢复请求须返回同一结果，并与调度器共用 Agent 执行槽。

API/UI 的后续实现顺序为：原子关联 → 按 occurrence 查询执行摘要 → 对账与恢复操作 → 设置页处理入口。没有可靠归属的旧 occurrence 显示“历史记录缺少关联，需核对”，不自动归并或重跑。页面显示可读日期、执行阶段和原因，revision/receipt ID 作为请求校验与诊断字段，不作为主要操作文案。

### 2026-09-09 恢复实现复核

原子归属、手动与每日执行隔离、快照 CAS 恢复及设置页入口已提交（361494b3、b5430604、9e5ad6f2、cfaca34b、ddc5e3cd）。GET 返回该 occurrence 的执行摘要与快照 token；POST 在共享执行槽内核对实际 memory operation 的 Agent、游标、输入及候选摘要，再恢复同一个 occurrence。已知且尚未开始的 prepared 子执行可以复用原预留；running、未知用量和缺失证据仍拒绝恢复，不自动重调模型。

主 Agent 本次独立回归：前端 70 文件 / 401 项通过；API 全包 race 通过（65.951 秒，明确排除已有 Windows 截图库 `TestScreenAPI_`）；handbookfs 全包通过。浏览器确认设置页仍连接旧 Node，维护、手册、事件接口返回不支持提示，因此不能将此次浏览器检查当作新版链路验收。

running 对账的轮次绑定基础已补齐：child 保存实际 TurnID 与 AttemptedAt，session 在 SQLite 写入 turn.started 后、模型调用前完成 Goals 绑定；绑定失败取消该轮次且不调用模型。绑定只接受同 Agent、同 Session、有效父子关联的 running child，重复同 TurnID 保持原记录，改绑或磁盘失败不改变记录。主 Agent 独立 Goals 全包 race（2.646 秒）、Session 手册专项 race（5.705 秒）及 HTTP/调度/重启 prepared 专项 race（4.797 秒）通过；HTTP 测试验证重开后仍能按绑定查询对应 turn.started。

文件历史来源已接入：controller 注入实际 child receipt ID，session 在绑定成功后填入真实 session/turn ID，文件工具将 context 传入 handbookfs。manifest 与 pending 事务记录同一可选 provenance。旧记录与普通操作不推测来源；恢复旧内容使用本次恢复的来源，不继承旧版本归属。已提交 manifest 与残留 pending 的来源冲突会阻止清理，未提交事务继续按既有规则回滚，不能补写虚假的成功历史。

主 Agent 独立来源验收：handbookfs 全包 race 通过（1.815 秒），真实 HTTP 维护入口的注入模型写文件测试通过（2.392 秒），读取文件历史确认实际 receipt/session/turn 三项一致；Session 手册专项与工具手册专项 race 分别通过（5.757 / 2.533 秒）。这些使用测试模型，不计为真实提供商验收。

完整事件及费用核对器仍未实现。下一步复用 Coordinator 的事件回放，增加有界读取、完整性验证及严格终态检查，不另建重复状态机。历史 child 没有 TurnID 时不能推测归属。仅有 assistant 文本、文件改动或进程退出均不能认定完成；缺 terminal 或完整用量证据时继续保留待处理及未知费用。此项完成前，阶段 F 和发布验收仍未完成。

## 剩余集成门槛

1. 共享执行槽与手动维护 API 已提交（dc483f17）。聊天/业务优先；取消维护不等于实际退出，函数返回后才释放槽。每日 controller 仍需证明服务关闭时也遵循这一规则。
2. 每日 controller 已提交（a3a26d40）。独立 API race 回归通过（48.383s，排除已有 Windows 截图库 TestScreen 问题），包含阻塞模型调用的关闭与 HTTP 维护停用取消、忙时不领取、重启 pending 不重调模型、九段快照跨批次续跑。其他配置变更入口的取消覆盖及真实模型运行仍需完成。
3. API/UI 需提供可信历史、下一安排和恢复原因。显示累计维护用量，未知用量显示待对账，不把累计值标成单次消耗。
4. 用户于 2026-09-09 明确要求手册采用文件系统：指定目录，模型自主创建层级和修改文件，不使用数据库。此前 SQLite 手册源代码已移除（01a04e49），Memory 全包 race 独立通过（8.797s），未删除实际数据库。运行时、工具、API/UI 正在改为绑定目录与文件浏览/编辑，修改历史也使用文件。事实记忆数据库保持独立。
5. 发布前验证旧版本拒绝读取不兼容新状态、重启和故障恢复、真实配置 LLM、至少一次每日维护与两个岗位周期，以及 Node/Manage 联调和视觉验收。

## 独立证据

完成快照 d0c7f416、记忆事务 3e3d47d0、receipt 恢复 22cde74e。日程存储全 Goals race 独立通过（2.191s），覆盖生效起点、重复领取、恢复、跨日阻断、重新启用、普通 profile 修改及 DST。

手动 API 两段快照测试已独立通过：提取两次，第三次无新增跳过，cursor 1/2/2，累计维护用量 6；使用注入提取器，不计作真实 LLM 验收。后续结果以[实施台账](2026-09-08-auto-employee-implementation-progress.md)为准。

2026-09-09 复核：Goals 全包 race 通过（2.278s）；Memory 全包 race 通过（13.100s）；NavRail 与 AgentMaintenancePanel 共 5 项前端定向测试通过。这些结果不覆盖尚在开发的手册 HTTP 资源收尾、每日 controller 生命周期及真实模型维护，不能用于宣称阶段 F 完成。

2026-09-09 后续复核：手册父子执行状态、唯一领取、绑定会话、实际用量结算与写失败回滚已提交（861923ae），主 Agent 独立 Goals 全包 race 通过（2.439s）。维护结果界面已改为显示服务端实际状态（cdcac77d），15 项定向测试独立通过；pending 或未知状态不再显示“已完成”。这两项不代表每日恢复完成：控制器的批次收尾、可信维护日志前缀推进及重启集成仍在修改和回归中，真实 LLM 与全页视觉验收仍未完成。
