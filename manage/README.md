# Manage 统一控制面

Manage 是 DAgents 的 **Python 控制面服务**，管理所有注册的 Agent Node。

| 域 | 状态 | 说明 |
|----|------|------|
| **Platform** | M0 | 鉴权、审计、Blob 占位、指标 |
| **Registry** | M1 | 注册、心跳、注销、目录、discover |
| **A2A** | **M2 部分** | Task API + inbox long poll；Node inbox poller 骨架 |
| **Skills** | M3 待做 | 一 zip 一 skill，审批后分发 |
| **Console** | **部分** | **`GET /console/`** — Node 目录、**A2A Inbox 只读列表**、Node 抽屉内 **session 列表 / context 摘要** |

架构方案：[docs/design/manage-architecture.md](../docs/design/manage-architecture.md)

## 启动

```bash
# 仓库根目录
python run_manage.py
```

默认 **`0.0.0.0:8020`**（`MANAGE_HOST` / `MANAGE_PORT` 可配置）。

**Console（Node 目录 UI）**：浏览器打开 **`http://<host>:<port>/console/`**  
基于 **Vue 3 + Vite**；源码在 `manage/console/frontend/`，构建产物在 `manage/console/static/`。  
修改 UI 后执行 `./manage/console/build.sh`（或 `cd manage/console/frontend && npm run build`）。  
默认 **开放模式**：无需 token，直接查看全部 Node 状态。

## Docker 部署（推荐生产 / 联调）

官方镜像与 compose 见 **[`packaging/manage/`](../packaging/manage/README.md)**。

### 联网快速启动

```bash
docker build -f packaging/manage/Dockerfile -t dagents-manage:0.3.7 .
# 或
cd packaging/manage && cp .env.example .env && docker compose up -d --build
```

打 **`v*`** 标签 Release 时会附带 **`dagents-manage-<version>.tar.gz`**（`docker load` 离线导入）。

### 离线安装（内网 / 无公网）

目标机需已安装 **Docker**（或 Docker Compose），但无法访问 Docker Hub 与 GitHub。

**步骤 1 — 联网机准备镜像包**

| 方式 | 命令 / 说明 |
|------|-------------|
| Release 下载 | 从 [GitHub Releases](https://github.com/DGS-ai-team/DAgents/releases) 获取 `dagents-manage-<version>.tar.gz` |
| 本地构建导出 | `VERSION=0.3.7 bash scripts/ci/build_manage_docker.sh` → `dist/dagents-manage-0.3.7.tar.gz` |
| 手动导出 | `docker save dagents-manage:0.3.7 \| gzip -9 > dagents-manage-0.3.7.tar.gz` |

将 tar.gz 与（可选）`packaging/manage/docker-compose.yml`、`.env.example` 拷贝至离线机。

**步骤 2 — 离线机导入镜像**

```bash
docker load -i dagents-manage-0.3.7.tar.gz
docker image ls dagents-manage   # 确认 TAG 与后续启动命令一致
```

**步骤 3 — 启动**

```bash
# 单容器
docker run -d --name manage --restart unless-stopped \
  -p 8020:8020 -v manage-data:/data \
  -e MANAGE_DB_PATH=/data/manage.db \
  dagents-manage:0.3.7

# 或使用 compose（拷贝 packaging/manage/ 后）
cd packaging/manage
cp .env.example .env   # MANAGE_IMAGE=dagents-manage:0.3.7
docker compose up -d   # 离线环境勿加 --build
```

**步骤 4 — 验证**

```bash
curl -sf http://127.0.0.1:8020/health
```

Console：**`http://<host>:8020/console/`**

数据目录挂载在 volume `manage-data`（`/data/manage.db`）。停止 / 重启：

```bash
docker stop manage && docker start manage
# 或 docker compose stop / docker compose start
```

更完整的变量说明与联调配置见 [`packaging/manage/README.md`](../packaging/manage/README.md)。

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
| `MANAGE_A2A_INBOX_CONTENT_MAX_CHARS` | `4096` | inbox 返回 `content` 最大字符；超出截断并设 `content_truncated` |
| `MANAGE_A2A_EXPIRE_SWEEP_SECONDS` | `30` | 后台 TTL 过期扫描间隔；`0` 关闭（仅按需单条过期） |

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

### Admin 观测（只读）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/admin/a2a/tasks` | A2A Task 列表（**不会 deliver**） |
| GET | `/v1/admin/nodes/{agent_id}/sessions` | 代理 Node `GET /v1/sessions` |
| GET | `/v1/admin/nodes/{agent_id}/sessions/{session_id}/context` | 代理 Node context 摘要 |

## A2A Task API（M2）

创建 Task 时 Manage 校验：

1. target **online** 且 **expose_to_peers=true**
2. caller 与 target 均至少有一个 **discovery_group**，且**存在交集**（否则 `403 discovery_group_mismatch` / `caller_discovery_group_empty` / `target_discovery_group_empty`）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/a2a/tasks` | 创建 Task（`kind`: invoke \| notify） |
| GET | `/v1/a2a/inbox` | long poll 拉取 pending（`?wait=25`） |
| POST | `/v1/a2a/tasks/{id}/ack` | 标记 processing |
| POST | `/v1/a2a/tasks/{id}/reply` | 提交结果 |
| GET | `/v1/a2a/tasks/{id}` | 查询状态 |

协议说明：[docs/future/a2a-via-manage.md](../docs/future/a2a-via-manage.md)（**无** `/v1/a2a/messages` 兼容）。

## 目录结构

```text
manage/
  config.py
  manage_app.py
  admin/        # Admin 只读 API（A2A 列表、Node session 代理）
  platform/     # auth, audit, blob, metrics
  storage/      # sqlite
  registry/     # models, store, routes, status
  console/
    frontend/   # Vue 3 源码（Vite）
    static/     # 构建产物，挂载 /console/
    build.sh    # npm run build 封装
  a2a/          # M2 Task store + routes
  skills/       # M3 占位
```

## 历史 Register Center 数据迁移

旧版 **`register_center/`** 已移除；新功能请落在 **Manage**。若仍有 RC JSON 导出，可导入 Registry：

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
  a2a:
    enabled: true              # 默认随 manage.enabled 开启
    inbox_wait_seconds: 25     # long poll wait
    inbox_poll_seconds: 30     # 断线降级短 poll
```

**discovery_group** 不由 Node 传入；在 Manage Console 详情抽屉或 API 分配：

```bash
curl -X PATCH http://127.0.0.1:8020/v1/registry/agents/ops-linux-01/groups \
  -H 'Content-Type: application/json' \
  -d '{"discovery_group":["ops"]}'
```

开放模式下 **无需** `node_token`；启用 `MANAGE_TOKENS` 后再配置 token 与角色。

符号索引见 [REFERENCE.md](./REFERENCE.md)。
