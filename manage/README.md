# Manage 统一控制面

Manage 是 DAgents 的 **Python 控制面服务**，管理所有注册的 Agent Node。

| 域 | 状态 | 说明 |
|----|------|------|
| **Platform** | M0 | 鉴权、审计、Blob 占位、指标 |
| **Registry** | M1 | 注册、心跳、注销、目录、discover |
| **A2A** | M2 待做 | inbox 消息 + Blob 文件 |
| **Skills** | M3 待做 | 一 zip 一 skill，审批后分发 |
| **Console** | **部分（Registry 目录页）** | **`GET /console/`** Node 列表与详情 |

架构方案：[docs/design/manage-architecture.md](../docs/design/manage-architecture.md)

## 启动

```bash
# 仓库根目录
python run_manage.py
```

默认 **`0.0.0.0:8020`**（`MANAGE_HOST` / `MANAGE_PORT` 可配置）。

**Console（Node 目录 UI）**：浏览器打开 **`http://<host>:<port>/console/`**  
默认 **开放模式**：无需 token，直接查看全部 Node 状态。

## 鉴权（当前 MVP）

| 模式 | 条件 | 行为 |
|------|------|------|
| **开放模式** | 未设置 `MANAGE_TOKENS` 且未设置 `MANAGE_SHARED_TOKEN` | Console / 列表 API 无需鉴权；Node 注册/心跳只需 **agent_id**（建议 Header `x-dagents-agent-id`） |
| **Token 模式** | 配置了上述环境变量 | 启用 admin/member/node 角色（后续完善；权限仍保留在 Manage 端） |

Node 出站 Header：

- **`x-dagents-agent-id`**：与 JSON 体 `agent_id` 一致（当前主要身份标识）
- **`x-dagents-a2a-token`**：可选，Token 模式启用后使用

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `MANAGE_HOST` | `0.0.0.0` | 监听地址 |
| `MANAGE_PORT` | `8020` | 监听端口 |
| `MANAGE_DB_PATH` | （空=仅内存） | Registry SQLite 路径 |
| `MANAGE_BLOB_DIR` | （空） | Blob 根目录（M2/M3） |
| `MANAGE_BLOB_MAX_BYTES` | （空=不限制） | 单 Blob 上限，字节 |
| `MANAGE_OFFLINE_GRACE_SECONDS` | `86400` | TTL 过期后 offline 保留秒数 |
| `MANAGE_TOKENS` | （空） | **可选**；JSON 角色/token 配置（后续启用 RBAC 时使用） |
| `MANAGE_SHARED_TOKEN` | （空） | **可选**；单 shared admin token |
| `MANAGE_AUDIT_PATH` | （空） | 审计 JSONL 追加路径 |
| `MANAGE_AUDIT_MAX_ENTRIES` | `500` | 内存审计条数 |
| `MANAGE_LEGACY_DIRECT_RELAY` | `0` | M2 前无效；启用 RC 式直连 relay 适配（默认关） |

鉴权 Header（Token 模式）：`x-dagents-a2a-token` 或 `Authorization: Bearer …`。  
身份 Header（Node 注册）：`x-dagents-agent-id: <agent_id>`。

## Registry API（M1）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/registry/agents` | 注册 / upsert |
| POST | `/v1/registry/agents/{id}/heartbeat` | 心跳 |
| POST | `/v1/registry/agents/{id}/deregister` | 注销 |
| PATCH | `/v1/registry/agents/{id}/groups` | **Manage 分配** discovery_group |
| GET | `/v1/registry/agents` | 列表（分页/筛选） |
| GET | `/v1/registry/agents/discover` | A2A 发现（**不含 base_url**） |
| GET | `/v1/registry/agents/{id}` | 详情 |
| DELETE | `/v1/registry/agents/{id}` | 管理员删除 |

系统：`GET /health`、`GET /metrics`、`GET /v1/admin/audit`。

## 目录结构

```text
manage/
  config.py
  manage_app.py
  platform/     # auth, audit, blob, metrics
  storage/      # sqlite
  registry/     # models, store, routes, status
  console/static/  # Manage Console Web UI
  a2a/          # M2 占位
  skills/       # M3 占位
```

## 与 Register Center 的关系

`register_center/` **尚未删除**（计划 M5）；新功能请落在 **Manage**。可从 RC JSON 导入：

```python
from manage.registry.store import AgentRegistryStore
from manage.storage.sqlite import SQLiteDatabase
# AgentRegistryStore.import_rc_json(db, json.load(...))
```

## Go Node 自动注册

Node 启动后向 Manage **POST 注册**并 **周期心跳**；请求带 **`x-dagents-agent-id`**（值为配置中的 `agent_id`）。停机时 **deregister**。

```yaml
manage:
  enabled: true
  url: http://127.0.0.1:8020
  registration:
    base_url: http://192.168.1.10:18765   # 上报 Manage；local.endpoint 仍可为本机 127.0.0.1
    interval_seconds: 30
    ttl_seconds: 60
    team: platform
```

**discovery_group** 不由 Node 传入；在 Manage Console 详情抽屉或 API 分配：

```bash
curl -X PATCH http://127.0.0.1:8020/v1/registry/agents/ops-linux-01/groups \
  -H 'Content-Type: application/json' \
  -d '{"discovery_group":["ops"]}'
```

开放模式下 **无需** `node_token`；启用 `MANAGE_TOKENS` 后再配置 token 与角色。

符号索引见 [REFERENCE.md](./REFERENCE.md)。
