# `app/harness/streaming/` REFERENCE

## `events.py`

- **`StreamEvent`**：**Pydantic `BaseModel`（frozen）**；统一流事件（`client_id/session_id/type/seq/ts/data`），**`to_dict` → `model_dump`**
- **`_RequestStream`**：内部流元信息容器（非 Pydantic；保存 `session_id/client_id`）
- **`EventBus`**：事件总线协议（可替换内存/Redis 实现）
- **`InMemoryEventBus`**：内存事件总线实现（当前默认）；仅保留 `subscribe_all(client_id)` 全局订阅

