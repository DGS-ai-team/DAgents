# node/internal/triggers

Go Node 触发器：JSON 持久化、调度轮询、fire 投递 session 队列。

| 文件 | 说明 |
|------|------|
| `models.go` | Definition / FireRecord、condition 校验（interval / fire_at / schedule 互斥）、`session_target_mode` |
| `session_target.go` | 审批选项 → 持久化/fire override 映射 |
| `schedule.go` | 结构化 `schedule` 日历调度、漏触发判定、`RescheduleNextFire` |
| `cmd_gate.go` | schedule 自动触发前可选 bash cmd 门控（exit 0 通过） |
| `store.go` | triggers.json CRUD、history、`ListEnabledTriggers` / `ReplaceTrigger` |
| `scheduler.go` | 后台 poll、`EvaluateDue`、`FireTrigger`、cmd 门控 wiring |
| `delivery.go` | 进程内 pending 投递去重（不写入 JSON） |
| `template.go` | task_template 占位符渲染 |
| `logging.go` | trigger 创建/更新/删除与 fire 结果的结构化日志 |
| `schedule_test.go` | 日历调度、漏触发、cmd 门控单测 |

## condition 调度类型

`interval_seconds`、`fire_at`、`schedule` **三选一**，不可组合。

| 键 | 说明 |
|----|------|
| `interval_seconds` | 固定间隔（秒） |
| `fire_at` | 单次 Unix 秒时间戳 |
| `schedule` | 日历调度（`daily` / `weekly` / `monthly`），时区跟随主机 `time.Local` |
| `cmd` | 可选；**仅 schedule 自动触发**时先 bash 执行，exit 0 才投递任务 |

### schedule 字段

| kind | 必填字段 |
|------|----------|
| `daily` | `hour`, `minute` |
| `weekly` | `weekday`（0=周日 … 6=周六）, `hour`, `minute` |
| `monthly` | `day`, `hour`, `minute` |

### 示例

周期 + 门控：

```json
{
  "schedule": {"kind": "weekly", "weekday": 0, "hour": 10, "minute": 0},
  "cmd": "test -f /tmp/ready"
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
- cmd 门控失败时同样推进 `next_fire_at`，不投递任务

## session_target_mode

| 值 | 行为 |
|----|------|
| `fixed`（缺省） | 使用 `target_session_id` |
| `new_session` | 首次 fire 新建 session 并写回为 `fixed` |
| `latest_active` | 每次 fire 动态解析最新活跃用户 session |

## 相关入口

- HTTP：`node/internal/api/` → `/v1/triggers*`
- Agent 工具：`node/internal/tools/triggers.go`
- 配置：`shared/config` 的 `triggers.enabled` / `poll_seconds`；存储路径固定 `{fs_root}/triggers/triggers.json`

## 日志

Node 启动时对 store/scheduler 注入 logger（见 `server.go`）。`log.level: info` 可见：

- `trigger created` / `trigger updated` / `trigger deleted`
- `trigger scheduler started`
- `trigger fired`（queued）/ `trigger fire skipped` / `trigger fire failed`

`debug` 额外输出 calendar 漏触发窗口外的 `trigger schedule advanced only`。

符号索引见同目录 [`REFERENCE.md`](./REFERENCE.md)。
