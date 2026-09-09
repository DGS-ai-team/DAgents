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

事件证据的只读核对已实现：读取限定事件数和 UTF-8 payload 字节数，超限返回错误而不是部分成功；使用现有 Coordinator 回放，先校验身份、起点、连续序列、版本、JSON 和终态之后的非法事件。只有成功终态、完整模型用量及无未解决工具或交互时，轮次证据才标为 completed。失败/取消终态即使用量已知也不算成功。

恢复 GET 对 running/recovery child 返回可选 reconciliation 摘要，最多读取 8 个 child，每个 4096 条事件、4 MiB payload，共享 30 秒期限。GET 不更改 receipt、费用或 occurrence；摘要 completed 仅表示轮次事件证据，不代表文件修改已核对或可直接恢复。主 Agent 独立 Store/Turn 全包 race 分别通过（5.394 / 9.590 秒），API 全部 MaintenanceRecovery 专项通过（6.381 秒），含真实 SQLite 完整 journal 与缺用量的 GET 分支；这些仍是合成事件测试，不算真实 LLM 验收。

显式结算动作仍需将事件证据、文件历史及当前 receipt 快照一起核验并原子记录。历史 child 没有 TurnID 时不能推测归属。仅有 assistant 文本、文件改动或进程退出均不能认定完成；缺 terminal 或完整用量证据时继续保留待处理及未知费用。此项完成前，阶段 F 和发布验收仍未完成。

### 显式结算的存储基础

新增 `ReadSourceSnapshot`，按完整 receipt/session/turn 来源读取已提交文件历史并生成稳定摘要。共享根目录锁下检查 pending，存在未完成事务时只读拒绝，不执行回滚；扫描最多 10000 条、8 MiB 历史，校验路径、递增 revision、摘要及时间，空来源结果统一编码为 `[]`。它只证明已提交历史归属，不证明模型轮次成功，也不保证文件仍等于某个历史版本。

新增 `ReconcileHandbookCompletion` 存储方法，使用 occurrence 全快照 token 做 CAS，原子保存 child、parent 和维护费用。首次写入仅允许待恢复 occurrence 下已绑定的 running/pending child，或费用一致的 settled/known/recovery child；新证据保存在 `ReconciliationEvidence`，原 `ResultJSON` 保留。同请求凭保存的 token 和规范化证据幂等，允许随后 Resume 或重开后重试；失败完整回滚。此方法不读取事件或文件，其调用方必须先核对两类证据，不能直接接受客户端上报的完成结论或用量。当前尚未接入 HTTP 写操作。

本轮发现旧维护 unknown 仅写全局标记，没有可归属的未知贡献；因此上述方法拒绝 unknown child，并保留所有全局 unknown 状态，不能用一个 child 的对账清除其他业务或历史未知费用。来源追踪与旧未知状态的明确处置仍是未完成项，不以本次存储基础替代完整恢复。

主 Agent 独立 Goals 全包 race 通过（3.049 秒），包含并发单次计费、过期快照、unknown 拒绝、保存失败完整快照回滚、Resume 后重试以及大 JSON 缩进后重开的幂等验证；handbookfs 全包 race 通过（2.389 秒）。HTTP 显式结算、界面及真实运行验收仍待继续。

### HTTP 显式结算接入

新增 POST `/v1/agents/{agent_id}/maintenance/reconcile`，请求只带 occurrence 坐标、快照 token 与子执行 ID。服务端在共享执行槽内核对实际 memory operation/游标、完整轮次事件和手册来源历史，自行计算用量与证据，再调用上述原子方法。成功只结算 child，occurrence 仍待恢复；精确重试读取已保存结论，不再计费。跨 Agent 真实 receipt、过期 token、执行槽繁忙、缺用量、缺目录及 pending 文件均拒绝。

执行前通过 `BindHandbookTurnWithRoot` 保存 Registry 实际绑定且已解析的绝对手册根目录。以后配置改目录不改变旧执行归属；旧 child 缺 root 时不能使用当前目录代替。证据读取使用 `OpenReadOnly`，不会创建目录或自动回滚 pending 事务。设置页对轮次记录完整的 child 提供逐项“核对并结算”，展示该轮用量，成功刷新摘要且保留未保存草稿；继续整个批次仍是独立动作。

主 Agent 独立复验：Goals 全包 race 3.180 秒；handbookfs 全包 race 2.444 秒；API Reconcile/Recovery 专项 race 10.860 秒，涵盖真实 SQLite journal、memory Apply、文件来源、幂等、跨 Agent 及拒绝时不变；面板 23 项测试及前端构建通过。上述是集成测试与构建证据，浏览器所连旧 Node 尚未更新，不能据此宣称新版视觉或真实 LLM 验收完成。

仍需补齐：journal 含写工具却没有来源历史时暂时保守拒绝，这也涵盖部分无实际修改的 no-op 写入，后续需结构化工具结果区分；未知费用来源追踪与处置未完成。全计划的真实循环、每日维护、故障注入和 Node/Manage 最终验收继续保留。

### 无实际修改写入的结构化证据与暂停

`write_file` 字节相同或 `search_replace` 结果不变时，维护工具通过 recorder 记录实际文件摘要，不生成虚假的修改历史。Session 将其保存为 `handbook.noop` 外部事实，绑定 receipt/session/turn/tool call、工具名与根内路径，并使用稳定命令 ID。记录失败向工具调用传播错误。来源历史为空时，API 要求每个写调用存在唯一匹配事实；旧日志没有此证据仍保守拒绝。

主 Agent 独立复验 Tools/Session/API 维护专项 race 分别通过（2.331 / 7.310 / 12.078 秒）；补齐最后接缝后 Session no-op 与 API no-op 专项 race 再次通过（2.593 / 2.174 秒）。接缝测试通过真实 Session、工具执行和 SQLite 原始事件验证 API 的字符串载荷解码；模型为注入测试客户端，不属于真实提供商验收。Tools 覆盖 recorder 错误传播，Session 另覆盖存储不可用时执行失败，两者不等同于完整生产故障注入。

用户要求本项收尾后暂停开发。完整状态及未闭环项见[暂停交接](2026-09-09-auto-pause-handoff.md)。下方较早的门槛记录保留为历史，不能覆盖本节及前述已提交事实。

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
