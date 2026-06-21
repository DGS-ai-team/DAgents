# REFERENCE — `node/internal/triggers`

| 符号 | 说明 |
|------|------|
| `Definition` / `CreateInput` / `UpdatePatch` / `FireRecord` | 触发器模型 |
| `ScheduleKind` / `CalendarKind` / `ScheduleSpec` | 调度类型与日历 spec |
| `EnsureScheduleCondition` | 校验 condition 含 interval_seconds、fire_at 或 schedule |
| `ParseScheduleSpec` / `NextCalendarFire` / `ComputeNextFireTime` | 日历 next_fire_at 计算 |
| `EvaluateDue` / `DueDecision` / `RescheduleNextFire` | 漏触发补发 vs 仅推进 |
| `ConditionCmd` | 读取 condition 内可选 cmd |
| `CmdGate` / `ShellCmdGate` | schedule 触发前 bash 门控（exit 0 通过） |
| `OpenStore(path, historyLimit)` | 打开 JSON 存储 |
| `Store.SetLogger` | 注入 logger（create/update/delete 打 Info） |
| `Store.ListTriggers` / `GetTrigger` / `CreateTrigger` / `UpdateTrigger` / `DeleteTrigger` | CRUD |
| `Store.ListEnabledTriggers` / `ReplaceTrigger` | 调度 tick 内部更新 |
| `Store.HasPendingDelivery` / `MarkPendingDelivery` / `ClearPendingDelivery` | trigger 待投递去重；**Clear** 在 side-effect Apply 成功或 ClearSession 丢弃时 |
| `DeliveryTracker` | 待消费跟踪接口 |
| `MessageSubmitter` | fire 时创建 session 并入队 |
| `NewScheduler` / `Start` / `Stop` / `FireTrigger` / `SetCmdGate` / `SetLogger` | 调度器 |
| `RenderTaskTemplate` | 渲染 task_template |
