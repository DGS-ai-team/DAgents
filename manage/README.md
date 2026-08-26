# Manage 统一控制面

Manage 是 DAgents 的 **Python 控制面服务**，管理所有注册的 Agent Node。

| 域 | 状态 | 说明 |
|----|------|------|
| **Platform** | ✅ | 鉴权、审计、Blob、指标 |
| **Registry** | ✅ | 注册、心跳、注销、目录、discover |
| **Workgroup** | ✅ | 跨 Node 协作（Leader + Worker Dialer）；**D6**：Manage Leader LLM loop（Mock/真实）+ `assign_workgroup_task` |
| **A2A Task/Inbox** | ❌ 已拆除 | 原 inbox / `agent_invoke` / Placement control 已移除；跨机请用工作组 |

| **Skills / Plugins / ExternalTools** | ✅（Manage 侧） | 精简分发（draft → publish）；**Node 自动 sync 待 Phase 2** |
| **LLM** | ✅（Manage 侧） | 集中 CRUD + `/resolve`；**Node 自动消费待做** |
| **Releases** | ✅ | 安装包托管 + `/v1/releases/check`；Node `UpdateChecker` |
| **Cases** | ✅ | 案例库（JSONL 演示会话 + 关联资源） |
| **Console** | ✅ | Agent 目录、案例库、Node 配置（LLM/Skills/Plugins/ExternalTools/版本发布） |

架构方案：[docs/design/manage-architecture.md](../docs/design/manage-architecture.md)

## 启动

```bash
# 仓库根目录
python3 run_manage.py
```

依赖约束见根目录 `requirements.txt`，实际安装使用 `requirements.lock`（含 **`websockets`**：工作组 Dialer WS 必需；缺库时握手 404，成员会一直停在「配置中」）。

默认 **`0.0.0.0:8020`**（`MANAGE_HOST` / `MANAGE_PORT` 可配置）。

**Console（Node 目录 UI）**：浏览器打开 **`http://<host>:<port>/console/`**  
基于 **Vue 3 + Vite**；源码在 `manage/console/frontend/`，构建产物在 `manage/console/static/`（**不入库**，CI / Docker 多阶段构建；本地运行 Manage 或跑 Python 单测前须先 build）。  
修改 UI 后执行 `./manage/console/build.sh`（或 `cd manage/console/frontend && npm run build`）。  
默认 **开放模式**：Node 注册/心跳仍可无 token；**Console 浏览器**需登录：
- Shell「打开 Manage」会带 `?node_id=`，若该 id 已在 Registry 登记则直接进入首页；
- 直接打开 `/console/` 需管理员账号密码（默认 `admin` / `admin`，可用 `MANAGE_ADMIN_USERNAME` / `MANAGE_ADMIN_PASSWORD` 覆盖）。

## Docker 部署（推荐生产 / 联调）

官方镜像与 compose 见 **[`packaging/manage/`](../packaging/manage/README.md)**。

### 联网快速启动

```bash
docker build -f packaging/manage/Dockerfile -t dagents-manage:0.5.1 .
# 或
cd packaging/manage && cp .env.example .env && docker compose up -d --build
```

打 **`v*`** 标签 Release 时会附带 **`dagents-manage-<version>.tar.gz`**（`docker load` 离线导入）。

### 离线安装（内网 / 无公网）

目标机需已安装 **Docker**（或 Docker Compose），但无法访问 Docker Hub 与 GitHub。

**步骤 1 — 联网机准备镜像包**

| 方式 | 命令 / 说明 |
|------|-------------|
| Release bundle（推荐） | `dagents-manage-bundle-<version>.tar.gz`（镜像 + compose + `import-image` / `restart` 脚本） |
| Release 仅镜像 | `dagents-manage-<version>.tar.gz` |
| 本地构建 bundle | `VERSION=0.5.1 bash scripts/ci/assemble_manage_bundle.sh` |
| 本地仅镜像 | `VERSION=0.5.1 bash scripts/ci/build_manage_docker.sh` |

离线机解压 bundle 后：`bash scripts/import-image.sh && bash scripts/restart.sh`（详见 [`packaging/manage/README.md`](../packaging/manage/README.md)）。

**步骤 2 — 验证**

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
| **开放模式** | 未设置 `MANAGE_TOKENS` 且未设置 `MANAGE_SHARED_TOKEN` | Node 注册/心跳只需 **agent_id**；无会话 cookie 时 API 仍可匿名（兼容自动化）；**Console UI** 强制会话登录 |
| **Token 模式** | 配置了上述环境变量 | 启用 admin/member/node 角色（后续完善；权限仍保留在 Manage 端） |
| **Console 会话** | Cookie `dagents_manage_session` | 管理员密码登录，或已注册 `node_id` 免密进入（权限绑定该 Node 的 discovery_group） |

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
| `MANAGE_ADMIN_USERNAME` | `admin` | Console 管理员账号 |
| `MANAGE_ADMIN_PASSWORD` | `admin` | Console 管理员密码（生产务必修改） |
| `MANAGE_AUDIT_PATH` | （空） | 审计 JSONL 追加路径 |
| `MANAGE_AUDIT_MAX_ENTRIES` | `500` | 内存审计条数 |

鉴权 Header（Token 模式）：`x-dagents-a2a-token` 或 `Authorization: Bearer …`。  
身份 Header（Node 注册）：`x-dagents-agent-id: <agent_id>`。

## Registry API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/registry/agents` | 注册 / upsert |
| POST | `/v1/registry/agents/{id}/heartbeat` | 心跳 |
| POST | `/v1/registry/agents/{id}/deregister` | 注销 |
| PATCH | `/v1/registry/agents/{id}/groups` | **Manage 分配** discovery_group |
| GET | `/v1/registry/agents` | 列表（分页/筛选） |
| GET | `/v1/registry/agents/discover` | 发现在线 Node（**不含 base_url**） |
| GET | `/v1/registry/agents/{id}` | 详情 |
| DELETE | `/v1/registry/agents/{id}` | 管理员删除 |

系统：`GET /health`、`GET /metrics`、`GET /v1/admin/audit`。

> **已拆除**：A2A Task / Inbox、Placement `/v1/control/*`、Console Inbox、Node `agent_invoke`；跨机器协作请用工作组。  
> **已禁用**：Admin session 代理（`/v1/admin/nodes/.../sessions`）已移除，Manage 不再出站访问 Node session API。

## Release Hub（安装包托管）

Manage 在 `MANAGE_RELEASES_DIR`（默认 `/data/releases`）托管 `dagents-local-assistant` 安装包。Console → **管理 → 版本发布** 可上传草稿、发布、设为 latest。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/releases/packages` | Admin 上传（默认 draft） |
| GET | `/v1/releases/packages` | 列表 |
| POST | `/v1/releases/packages/{artifact}/{channel}/{platform}/{version}/publish` | 发布 |
| POST | `/v1/releases/packages/{artifact}/{channel}/{platform}/{version}/promote` | 设为 latest |
| GET | `/v1/releases/check` | Node 版本检查 |
| GET | `/v1/releases/packages/.../latest/download` | 下载 latest |

发版时 CI 将同版本 `dagents-local-assistant-linux-amd64-*.tar.gz` 打入 Manage Docker 镜像与 offline bundle（`/app/bundled/releases` seed）。详见 [docs/design/release-update-hub.md](../docs/design/release-update-hub.md)。

Node：`GET /v1/agent/update`（需 `manage.enabled`）。

### 案例库（Cases）

Console → **案例库**：上传 Node history JSONL 创建演示案例，可编辑消息（插入 / 修改 / 删除），并关联 Skills、Plugins。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/cases` | 案例列表 |
| POST | `/v1/cases` | 创建（multipart：`name` + 可选描述/资源/JSONL；**`case_id` 为服务端 `uuid4().hex`**） |
| GET | `/v1/cases/{case_id}` | 详情（含消息列表） |
| PATCH | `/v1/cases/{case_id}` | 更新名称、描述、关联资源 |
| DELETE | `/v1/cases/{case_id}` | 删除 |
| POST | `/v1/cases/{case_id}/import-jsonl` | 导入 JSONL（replace 或追加） |
| POST | `/v1/cases/{case_id}/messages` | 插入消息 |
| PATCH | `/v1/cases/{case_id}/messages/{id}` | 修改消息 |
| DELETE | `/v1/cases/{case_id}/messages/{id}` | 删除消息 |
| GET | `/v1/cases/{case_id}/export/jsonl` | 导出 JSONL |

JSONL 行格式对齐 Node `history/*.jsonl`：`{"recorded_at":"...","message":{"role":"user","content":"..."}}`。

## 目录结构

```text
manage/
  config.py
  manage_app.py
  platform/         # auth, audit, blob, metrics
  storage/          # sqlite
  registry/         # models, store, routes, status
  workgroup/        # 跨 Node 协作
  llm/              # LLM 配置注册中心
  skills/           # Skill 包分发
  plugins/          # Hook Plugin 分发
  externaltools/    # 外置工具分发
  releases/         # Release Hub
  cases/            # 案例库
  console/
    frontend/       # Vue 3 源码（Vite）
    static/         # 构建产物，挂载 /console/
    build.sh
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

通信与协作见 [docs/design/manage-architecture.md](../docs/design/manage-architecture.md)、[docs/user/workgroups.md](../docs/user/workgroups.md)。

```bash
curl -X PATCH http://127.0.0.1:8020/v1/registry/agents/ops-linux-01/groups \
  -H 'Content-Type: application/json' \
  -d '{"discovery_group":["ops"]}'
```

开放模式下 **无需** `node_token`；启用 `MANAGE_TOKENS` 后再配置 token 与角色。

符号索引见 [REFERENCE.md](./REFERENCE.md)。
