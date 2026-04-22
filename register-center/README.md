# register-center

Register Center 的实现目录（MVP：FastAPI + 内存存储）。

## 目录说明

| 路径 | 说明 |
|------|------|
| `__init__.py` | 包标记文件。 |
| `rc_models.py` | 请求/响应 Pydantic 模型与字段校验规则。 |
| `rc_store.py` | 进程内登记表存储（字典 + 线程锁）。 |
| `rc_app.py` | FastAPI 应用工厂与 REST 路由定义（含分组广播与单目标中继接口）。 |
| `REFERENCE.md` | 本目录 Python 符号索引。 |

## 运行方式

在仓库根目录执行：

```bash
python run_register_center.py
```

说明：

- 当前目录名为 `register-center`（含连字符），不便于作为常规 Python 包直接导入。
- 因此启动脚本 `run_register_center.py` 采用 `importlib` 按文件路径加载 `rc_app.py`。
- 这是现阶段兼容方案；后续若目录重命名为 `register_center`，可切换为常规包导入方式。

默认监听 `0.0.0.0:8010`，可通过环境变量覆盖：

- `REGISTER_CENTER_HOST`
- `REGISTER_CENTER_PORT`

## 核心接口

- `POST /v1/agents`：登记/更新 Agent（`discovery_group` 支持字符串或字符串列表，支持可选 `capabilities_hint`）
- `GET /v1/agents`：列表查询（`discovery_group` 必填，不提供全量视图）
- `GET /v1/agents/{agent_id}`：按 ID 查询（`discovery_group` 必填，分组隔离）
- `DELETE /v1/agents/{agent_id}`：按 ID 注销登记记录
- `POST /v1/broadcast`：按分组列表广播消息到已注册 Agent 的 `/v1/messages`
- `POST /v1/relay`：按 `target_agent_id` 中继单条消息到目标 Agent 的 `/v1/messages`
