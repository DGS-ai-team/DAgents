# node/internal/triggers

Go Node 触发器：JSON 持久化、调度轮询、fire 投递 session 队列。

| 文件 | 说明 |
|------|------|
| `models.go` | Definition / FireRecord、condition 校验（interval / fire_at / schedule 互斥）、`session_target_mode` |
| `session_target.go` | 审批选项 → 持久化/fire override 映射 |
| `schedule.go` | 结构化 `schedule` 日历调度、漏触发判定、`RescheduleNextFire` |
| `store.go` | triggers.json CRUD、history、`ListEnabledTriggers` / `ReplaceTrigger` |
| `scheduler.go` | 后台 poll、`EvaluateDue`、`FireTrigger`、目标运行时路由 |
| `delivery.go` | 投递确认接口；稳定投递身份由 Store 写入 JSON |
| `template.go` | task_template 占位符渲染 |
| `logging.go` | trigger 创建/更新/删除与 fire 结果的结构化日志 |
| `schedule_test.go` | 日历调度、漏触发、cmd 门控单测 |

## condition 调度类型

条件脚本由目标 Agent 的 session/turn 执行，复用现有 tool router、hooks、policy 和 HITL 边界；触发器调度器本身不直接执行宿主 shell。固定目标若未给 session，投递到已注册 Agent 的 canonical runtime；目标 Agent 不存在、已归档或无法加载时拒绝，不隐式创建跨 Agent runtime。每次投递先在 JSON 中 claim 稳定 `pending_delivery_id`，再写入 InputBox；消费确认携带 delivery identity，迟到确认不会清理后续投递。条件审批还会绑定 Agent、session、trigger revision、delivery 和 occurrence，恢复时逐项 CAS 校验。

`interval_seconds`、`fire_at`、`schedule` **三选一**，不可组合。

| 键 | 说明 |
|----|------|
| `interval_seconds` | 固定间隔（秒） |
| `fire_at` | 单次 Unix 秒时间戳 |
| `schedule` | 日历调度（`daily` / `weekly` / `monthly`），时区跟随主机 `time.Local` |
| `cmd` | 可选脚本门控；在目标 Agent 的 session/turn 中执行，复用 hooks、policy、HITL；退出码 0 才满足条件并投递 |

### condition.cmd 条件门控

`condition.cmd` 可用于手动、间隔、单次和日历触发。调度器先持久化 delivery claim，再进入目标 Agent 的条件 turn；策略为自动执行时才打开脚本，策略要求审批时先以普通 `execute_tool` HITL 形式持久化 `awaiting_approval`，审批完成前不会执行命令。审批恢复会校验 Agent、session、trigger revision、delivery、occurrence 及参数摘要；拒绝、非零退出码或执行失败会释放 claim，并按调度规则记录 skipped/error。脚本满足条件后，原始 task template 会投递到同一个主 session。

### schedule 字段

| kind | 必填字段 |
|------|----------|
| `daily` | `hour`, `minute` |
| `weekly` | `weekday`（0=周日 … 6=周六）, `hour`, `minute` |
| `monthly` | `day`, `hour`, `minute` |

### 示例

每周定时：

```json
{
  "schedule": {"kind": "weekly", "weekday": 0, "hour": 10, "minute": 0}
}
```

月末（`-1` = 当月最后一天；正数超出当月天数则跳月）：

```json
{
  "schedule": {"kind": "monthly", "day": -1, "hour": 8, "minute": 0}
}
```

### 漏触发

- `now >= next_fire_at` 且 `now - next_fire_at < 1 个周期` → **补发**
- 否则 → **只推进** `next_fire_at`（严格在 `now` 之后）
- 条件脚本退出码非 0 或执行失败时任务跳过/失败，不投递后续任务；审批未完成时保持 `awaiting_approval`，不会重复执行脚本。

## session_target_mode

| 值 | 行为 |
|----|------|
| `fixed`（缺省） | 使用所属 Agent 的指定会话；未指定时加载该 Agent 主会话 |
| `new_session` | 旧数据模式；已绑定会话按固定目标处理，未绑定的路由任务拒绝执行 |
| `latest_active` | 旧数据保留；路由任务拒绝执行，需显式改为固定目标 |

新建与修改 API 只接受固定目标。重启时未确认的投递会暂停并显示 `recovery_required`；`POST /v1/triggers/{trigger_id}/recover` 必须提交匹配的 `delivery_id`，只放弃旧投递且保持禁用。恢复后旧输入和旧任务继续执行消息均被拦截。

持久化故障时消费者停止以保留未确认记录。应先修复存储故障，再重启 Node 并核对相关任务；不能把 `queued` 当作任务完成，也不保证跨存储 exactly-once。

## 相关入口

- HTTP：`node/internal/api/` → `/v1/triggers*`
- Agent 工具：`node/internal/tools/triggers.go`
- 配置：`shared/config` 的 `triggers.enabled` / `poll_seconds`；存储路径固定 `<runtime_root>/triggers/triggers.json`

## 日志

Node 启动时对 store/scheduler 注入 logger（见 `server.go`）。`log.level: info` 可见：

- `trigger created` / `trigger updated` / `trigger deleted`
- `trigger scheduler started`
- `trigger fired`（queued）/ `trigger fire skipped` / `trigger fire failed`

`debug` 额外输出 calendar 漏触发窗口外的 `trigger schedule advanced only`。

符号索引见同目录 [`REFERENCE.md`](./REFERENCE.md)。
