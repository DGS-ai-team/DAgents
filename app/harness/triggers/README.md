# `app/harness/triggers/`

> **【已弃用 — Python 调度实现】** Go 完整实现：**[`node/internal/triggers/`](../../../node/internal/triggers/README.md)**。

定时/一次性/手动触发器：由 `condition` 推断调度类型，将 `task_template` 渲染为消息后投递到 `AgentService` 队列。

## 文件

| 文件 | 用途 |
|------|------|
| `models.py` | Pydantic 模型：`TriggerDefinition`、创建/更新入参、fire 记录与 API 列表包装 |
| `store.py` | `JsonTriggerStore`：`<运行根>/.runtime/triggers/triggers.json` 读写与到期查询 |
| `scheduler.py` | `TriggerScheduler`：轮询 `due_triggers`、统一 `fire_trigger` |
| `runtime.py` | 进程内单例 `get_trigger_store` / `get_trigger_scheduler` |
| `__init__.py` | 对外导出主要类型 |

## condition（Python MVP）

| 键 | 说明 |
|----|------|
| `interval_seconds` | 固定间隔（秒） |
| `fire_at` | 单次 Unix 秒时间戳 |

二者不可同时设置。日历 schedule 与 cmd 门控见 Go 实现文档：[`node/internal/triggers/README.md`](../../../node/internal/triggers/README.md)。

## 相关入口

- HTTP：`app/harness/api/app.py` → `/v1/triggers*`
- Agent 工具：`app/harness/tools/triggers.py`
- 配置：`TRIGGERS_ENABLED`、`TRIGGER_SCHEDULER_POLL_SECONDS`（见 `app/config/settings.py`）

## 设计文档

详见仓库 [`docs/triggers-design.md`](../../../docs/triggers-design.md)（长期架构）；当前实现为 MVP JSON 存储 + 轮询调度。
