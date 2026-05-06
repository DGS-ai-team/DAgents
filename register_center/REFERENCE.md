# register_center / REFERENCE

## `__init__.py`

- 模块说明：`Register Center 包。`

## `rc_models.py`

### 类

- `AgentUpsertRequest`
  - 说明：登记/更新请求模型，负责 `agent_id`、`base_url`、`discovery_group`（字符串或字符串列表）与 `capabilities_hint` 的校验与规范化。
  - 方法：
    - `validate_agent_id(value: str) -> str`
    - `validate_base_url(value: str) -> str`
- `AgentRecord`
  - 说明：对外返回的单条登记记录结构（`discovery_group` 为非空分组列表，可含 `capabilities_hint`）。
- `AgentListResponse`
  - 说明：列表查询响应壳，字段为 `agents`。
- `HealthResponse`
  - 说明：健康检查响应结构。
- `BroadcastRequest`
  - 说明：广播接口请求模型，包含 `message` 与 `discovery_group_ids`。
  - 方法：
    - `validate_message(value: str) -> str`
    - `validate_group_ids(value: list[str]) -> list[str]`
- `BroadcastResultItem`
  - 说明：单个目标 Agent 的广播结果。
- `BroadcastResponse`
  - 说明：广播接口聚合返回结构（成功/失败统计 + 明细列表）。
- `RelayRequest`
  - 说明：单目标中继请求模型，包含目标 ID、调用方分组与透传消息字段。
  - 方法：
    - `validate_target_agent_id(value: str) -> str`
    - `validate_non_empty_text_fields(value: str) -> str`
    - `validate_caller_groups(value: list[str]) -> list[str]`
- `RelayResponse`
  - 说明：单目标中继响应结构，含 `target_base_url` 与可选 `request_id`。

## `rc_store.py`

### 类

- `AgentRegistryStore`
  - 说明：基于内存字典的登记仓库，提供线程安全的增删改查与计数。
  - 方法：
    - `__init__() -> None`
    - `upsert(payload: AgentUpsertRequest) -> AgentRecord`
    - `get(agent_id: str) -> AgentRecord | None`
    - `list(discovery_group: str | None = None) -> list[AgentRecord]`
    - `delete(agent_id: str) -> bool`
    - `count() -> int`

## `rc_app.py`

### 函数

- `create_app() -> FastAPI`
  - 说明：构建 FastAPI 应用并注册 `/health`、`/v1/agents`（含 delete）、`/v1/broadcast`、`/v1/relay` 路由（查询接口按分组强隔离）。
- `_broadcast_to_agent(client, *, agent, message, source) -> BroadcastResultItem`
  - 说明：向单个 Agent 的 `/v1/messages` 转发广播消息并收集结果。

### 模块级变量

- `app`
  - 说明：`create_app()` 生成的默认应用实例，供启动脚本直接加载。
