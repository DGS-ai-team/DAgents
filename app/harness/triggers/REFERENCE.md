# `app/harness/triggers/` REFERENCE

## `models.py`

- **`ScheduleKind`**：调度类型字面量（由 condition 推断）
- **`infer_schedule_kind`**：从 `condition` 推断 manual / interval / once
- **`ensure_schedule_condition`**：校验 condition 非空且须为 interval 或 once
- **`TriggerDefinition`**：触发器资源；**`_validate_condition`**、**`schedule_kind`**、**`with_next_fire`**
- **`TriggerCreateIn`**：创建请求；**`to_definition`**
- **`TriggerUpdateIn`**：PATCH 部分更新
- **`TriggerFireRecord`** / **`TriggerFireIn`** / **`TriggerListResult`** / **`TriggerHistoryResult`**

## `store.py`

- **`JsonTriggerStore`**：JSON 文件存储（CRUD、**`due_triggers`**、**`mark_fired`**、history）

## `scheduler.py`

- **`TriggerScheduler`**：轮询与 fire（**`start`** / **`stop`** / **`fire_trigger`**）
- **`_render_task_template`** / **`_SafeFormatMap`**

## `runtime.py`

- **`get_trigger_store`** / **`set_trigger_runtime`** / **`get_trigger_scheduler`** / **`reset_trigger_runtime`**
