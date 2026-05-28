# `app/harness/triggers/`

定时/一次性/手动触发器：由 `condition` 推断调度类型，将 `task_template` 渲染为消息后投递到 `AgentService` 队列。

## 文件

| 文件 | 用途 |
|------|------|
| `models.py` | Pydantic 模型：`TriggerDefinition`、创建/更新入参、fire 记录与 API 列表包装 |
| `store.py` | `JsonTriggerStore`：`<运行根>/.runtime/triggers/triggers.json` 读写与到期查询 |
| `scheduler.py` | `TriggerScheduler`：轮询 `due_triggers`、统一 `fire_trigger` |
| `runtime.py` | 进程内单例 `get_trigger_store` / `get_trigger_scheduler` |
| `__init__.py` | 对外导出主要类型 |

## 相关入口

- HTTP：`app/harness/api/app.py` → `/v1/triggers*`
- Agent 工具：`app/harness/tools/triggers.py`
- 配置：`TRIGGERS_ENABLED`、`TRIGGER_SCHEDULER_POLL_SECONDS`（见 `app/config/settings.py`）

## 设计文档

详见仓库 [`docs/triggers-design.md`](../../../docs/triggers-design.md)（长期架构）；当前实现为 MVP JSON 存储 + 轮询调度。
