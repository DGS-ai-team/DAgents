# `app/harness/streaming/` REFERENCE

## `events.py`

- **`StreamEvent`**：**Pydantic `BaseModel`（frozen）**；统一流事件（`client_id/session_id/type/seq/ts/data`），**`to_dict` → `model_dump`**
- **`_RequestStream`**：内部可变缓冲（非 Pydantic；含 **`asyncio.Queue`**）
- **`EventBus`**：事件总线协议（可替换内存/Redis 实现）
- **`InMemoryEventBus`**：内存事件总线实现（当前默认）

