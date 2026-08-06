# Manage 统一控制面架构

> **状态**：v0.* 现网实现（`manage/`）  
> **2026-08**：A2A Task inbox / `agent_invoke` / Console Inbox **已拆除**；跨机协作改走 **工作组**。下文含历史 A2A 叙述处请以 handbook/05 为准。  
> **对齐**：[roadmap.md](../roadmap.md) · [handbook/05](../handbook/05-Manage与A2A.md)  
> **相关**：运维入口 [manage/README.md](../../manage/README.md) · 后续规划 [manage-phase2-capabilities.md](./manage-phase2-capabilities.md) · 通信叙述 [handbook/05](../handbook/05-Manage与A2A.md)

---

## 1. 定位

**Manage** 是 DAgents 的 **唯一 Python 控制面服务**：管理所有注册的 **Agent Node**，不参与 Client 会话与 LLM turn loop。

| 原则 | 说明 |
|------|------|
| **Node 仅出站连 Manage** | 注册、心跳、制品拉取、版本检查、工作组均为 **Node → Manage** |
| **禁止 Node-to-Node** | 跨 Agent 协作经 Manage（工作组）；discover API **不返回** peer 路由 URL |
| **Client 不连 Manage** | 运维通过 **Manage Console（Web）** 或 admin API |
| **Manage 不跑 turn** | 只做存储、目录、调度、中转；推理与工具执行仍在各 Node |
| **本地优先** | 默认 SQLite + 本地 Blob/Release 目录；生产可演进 PostgreSQL / S3 |

一句话：

> Manage = **Agent 接入目录** + **工作组协作** + **制品与配置分发** + **审计与运维控制台**。

---

## 2. 总体架构

```text
                         ┌─────────────────────────────────────────────┐
                         │              Manage Service                  │
                         │  (Python FastAPI, 默认 :8020)                │
                         ├─────────────────────────────────────────────┤
  Admin Browser ────────►│  Console (Web)  /console/*   OpenAPI /docs   │
                         ├──────────┬──────────┬───────────┬────────────┤
                         │ Registry │   A2A    │  制品分发  │  Platform  │
                         │  接入目录 │ 协作总线  │ Skills/…  │  横切能力  │
                         ├──────────┴──────────┴───────────┴────────────┤
                         │  Storage: SQLite metadata + Blob Store       │
                         └──────────────────▲───────────────────────────┘
                                            │ HTTPS 出站（Node 身份）
              ┌─────────────────────────────┼─────────────────────────────┐
              │                             │                             │
       ┌──────┴──────┐               ┌──────┴──────┐               ┌──────┴──────┐
       │ Agent Node  │               │ Agent Node  │               │ Agent Node  │
       │  (Go)       │               │  (Go)       │               │  (Go)       │
       └──────▲──────┘               └─────────────┘               └─────────────┘
              │
       ┌──────┴──────┐
       │   Client    │  仅连本机 Node
       └─────────────┘
```

### 2.1 域划分

| 域 | 前缀 | 职责 | Node 消费 |
|----|------|------|-----------|
| **Registry** | `/v1/registry` | Node 接入、心跳、目录、discover、分组 | ✅ 自动注册/心跳 |
| **A2A** | `/v1/a2a` | Node 间 Task inbox、HITL 中继 | ✅ InboxPoller + 工具 |
| **Skills / Plugins / ExternalTools** | `/v1/skills` 等 | 制品包上传、发布、目录、下载 | ⏳ 自动同步待做 |
| **LLM** | `/v1/llm` | LLM 配置集中 CRUD + resolve | ⏳ 自动消费待做 |
| **Releases** | `/v1/releases` | 安装包托管 + 版本检查 | ✅ UpdateChecker |
| **Cases** | `/v1/cases` | 演示会话（JSONL）+ 关联资源 | Console only |
| **Platform** | `/v1/blobs`、`/v1/admin`、`/health`、`/metrics` | 鉴权、审计、Blob、Console、指标 | Blob 共用 |

---

## 3. 模块设计

### 3.1 Registry（接入与目录）

**职责**

- Node **启动注册**、**周期心跳**、**优雅注销**（`node/internal/manage/registrar.go`）
- 维护 Agent 身份与能力清单
- **Discover**：供 A2A 工具查询可协作 Agent（`expose_to_peers=true` 且 online）
- **Admin 目录**：Console 全局列表、筛选、详情抽屉

**关键字段**

| 字段 | 谁写入 | 说明 |
|------|--------|------|
| `agent_id` | Node 配置 | 全局唯一；一进程一 id |
| `discovery_group[]` | **Manage**（PATCH groups） | 发现/协作分组；**Node 注册不传** |
| `expose_to_peers` | Node 配置 | 是否可作 A2A 被调方 |
| `capabilities[]` / `tools[]` | Node 心跳刷新 | 能力摘要 |
| `base_url` | Node `manage.registration.base_url` | **仅 Console 展示 / 人工跳转；A2A 路由不用** |
| `card` | Node `agent-card.json` + config | name / description / role |
| `status` | Manage 派生 | online / offline / expired（TTL grace） |

**API**（`manage/registry/routes.py`）

| 方法 | 路径 | 调用方 |
|------|------|--------|
| POST | `/v1/registry/agents` | Node 注册 / upsert |
| POST | `/v1/registry/agents/{id}/heartbeat` | Node 心跳 |
| POST | `/v1/registry/agents/{id}/deregister` | Node 停机 |
| PATCH | `/v1/registry/agents/{id}/groups` | Console / 脚本 分配 `discovery_group` |
| GET | `/v1/registry/agents` | Admin / member 列表（分页 / 筛选） |
| GET | `/v1/registry/agents/discover` | Node A2A 发现（**不含 base_url**） |
| GET | `/v1/registry/agents/{id}` | 详情（含 base_url，运维用） |
| DELETE | `/v1/registry/agents/{id}` | Admin 强制注销 |

**存储**：`registry_agents` 表；TTL + offline grace（`MANAGE_OFFLINE_GRACE_SECONDS`）。

---

### 3.2 A2A（协作总线）— Task + Inbox

**设计原则**

- **Task + Inbox 模型**：Manage 持久化工作单元；被调方 **long poll inbox**
- **无 peer URL**：调用方只持有 `to_agent_id`
- **大 payload 走 Blob**：正文外的文件引用 `blob_ids`

**Task API**（`manage/a2a/routes.py`）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/a2a/tasks` | 创建 Task（`kind`: invoke \| notify） |
| GET | `/v1/a2a/inbox?wait=` | 被调方 long poll |
| POST | `/v1/a2a/tasks/{id}/ack` | 标记 processing |
| POST | `/v1/a2a/tasks/{id}/reply` | 回复 / `requires_input` |
| GET | `/v1/a2a/tasks/{id}` | 调用方查状态 / 结果 |
| POST | `/v1/a2a/tasks/{id}/caller_notify` | HITL 中继：caller 已收到 |
| POST | `/v1/a2a/tasks/{id}/caller_resume` | HITL 中继：用户 resume |
| GET | `/v1/a2a/tasks/{id}/caller_input?wait=` | HITL 中继：callee 取 resume |

**创建校验**：`to` 存在、online、`expose_to_peers=true`；caller 与 target 的 `discovery_group` 存在交集；caller Header `x-dagents-agent-id` 与 `from_agent_id` 一致。

**状态机**

```text
queued → delivered → processing → completed | failed | expired
                       ↓
        awaiting_caller → caller_notified → caller_responded → processing → completed
```

**Node 侧**：注册 / 心跳 / Workgroup Dialer 出站连 Manage。跨机协作见 [handbook/05](../handbook/05-Manage与A2A.md)、[07](../handbook/07-Workgroup协作.md)。

**待做**：`POST /v1/a2a/broadcast`（按 groups 扇出）。

---

### 3.3 Blob（内容寻址存储）

统一 **Platform Blob API**（A2A payload、制品包、Cases 附件共用）：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/blobs` | multipart 上传；返回 `{blob_id, sha256, size}` |
| GET | `/v1/blobs/{id}` | 下载 |
| HEAD | `/v1/blobs/{id}` | 元数据 |
| DELETE | `/v1/blobs/{id}` | Admin 删除 |

`blob_id = sha256`（64 位小写十六进制），严格格式校验、拒绝路径穿越；未配置 `MANAGE_BLOB_DIR` 时返回 503。实现：`manage/platform/blob_routes.py`。

---

### 3.4 制品分发（Skills / Plugins / ExternalTools）

三类制品共享同一套路由与 store 结构，仅解压目标不同：

| 域 | 前缀 | Node 解压目标 |
|----|------|---------------|
| Skills | `/v1/skills` | `{fs_root}/skills/{skill_id}/` |
| Plugins | `/v1/plugins` | Hook `.so` 插件 |
| External Tools | `/v1/externaltools` | `.runtime/externaltools/` |

**生命周期**：**draft / published** 两态、单步发布。多级审批（pending_review → approved）暂不做。

**通用 API**（以 Skills 为例）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/skills/packages` | 上传新版本（draft） |
| POST | `/v1/skills/packages/{id}/versions/{ver}/publish` | 发布 |
| GET | `/v1/skills/catalog` | 已发布列表 |
| GET | `/v1/skills/catalog/{id}` | 元数据 + 版本列表 |
| GET | `/v1/skills/catalog/{id}/versions/{ver}/download` | 下载 zip |
| GET | `/v1/skills/sync/manifest?since=N` | 增量清单 `{catalog_version, items}` |

制品 zip 以 Blob 存储。Node 侧上传由 `package_uploader.go` 支持；**自动拉取 / 解压 / 热更未实现**（见 [manage-phase2-capabilities.md](./manage-phase2-capabilities.md) 能力市场）。

---

### 3.5 LLM 配置注册中心

集中管理 LLM 配置（provider / base_url / model / api_key），供多 Node 或浏览器端 PageAgent 按 id 复用。

**存储**：`llm_configs` 表。关键字段：`id`、`name`（唯一）、`provider`、`base_url`、`model`、`api_key`、`is_default`（全局至多一个）、`allowed_groups`。

**API**（`manage/llm/routes.py`）

| 方法 | 路径 | 鉴权 | 说明 |
|------|------|------|------|
| POST / PUT / DELETE | `/v1/llm/configs[/{id}]` | admin | CRUD |
| GET | `/v1/llm/configs[/{id}]` | authenticate | 列表 / 详情（`api_key` **掩码**） |
| GET | `/v1/llm/configs/{id}/resolve` | authenticate | **返回明文** `{model, baseURL, apiKey}` |
| GET | `/v1/llm/configs/default/resolve` | authenticate | 默认配置 resolve |

**安全模型**：`api_key` 明文存 SQLite，`/resolve` 明文返回——**仅适用于本地/局域网信任部署**。生产/公网须改为服务端代理 LLM、key 不出服务端。

**待做**：Go Node 自动消费（拉取 + turn `llm.settings` 热更）。

---

### 3.6 Releases（版本发布中枢）

Manage 托管 `dagents-local-assistant` 安装包，Node 周期查询升级、人工确认后本地安装（非静默强制）。

**API**（`manage/releases/routes.py`，前缀 `/v1/releases`）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/packages` | Admin 上传（默认 draft） |
| GET | `/packages` | 列表 |
| POST | `/packages/{artifact}/{channel}/{platform}/{version}/publish` | 发布 |
| POST | `/packages/{artifact}/{channel}/{platform}/{version}/promote` | 设为 latest |
| GET | `/check` | Node 版本检查 |
| GET | `/packages/{artifact}/{channel}/{platform}/latest/download` | 下载 latest |

Docker 启动时 `seed_bundled_releases` 可从 `/app/bundled/releases` 预置。Node 侧 `update_checker.go` 对接 `/check`，Node 暴露 `GET /v1/agent/update`。详见 [release-update-hub.md](./release-update-hub.md)。

---

### 3.7 Cases（案例库）

演示会话管理：上传 Node history JSONL、编辑消息（插入 / 修改 / 删除）、关联 Skills / Plugins / ExternalTools、附件 Blob、导出 JSONL。前缀 `/v1/cases`（`manage/cases/routes.py`）。创建时 **`case_id` 由服务端生成 `uuid4().hex`**；JSONL 行格式对齐 Node `history/*.jsonl`。

---

### 3.8 Platform（横切能力）

| 子模块 | 说明 | 状态 |
|--------|------|------|
| **Auth** | 开放模式（无 token）/ Token 模式（admin/member/node 角色） | MVP，RBAC 待生产化 |
| **Audit** | 内存审计 + 可选 JSONL；`GET /v1/admin/audit` | 无 Node ingest / timeline |
| **Blob** | 见 §3.3 | ✅ |
| **Metrics** | `GET /metrics`（Prometheus） | ✅ |
| **Console** | 见 §5 | ✅ |
| **Admin 观测** | `GET /v1/admin/a2a/tasks`（只读） | ✅ |

**待做**：Node 批量审计上报（`POST /v1/audit/events`）、按 session 串联的 timeline、配额 / 速率、Webhook。

---

## 4. 存储

**SQLite**（`manage/storage/sqlite.py`，schema v7）：

| 表 | 用途 |
|----|------|
| `registry_agents` | Agent 目录 |
| `a2a_tasks` | A2A Task |
| `llm_configs` | LLM 配置 |
| `skill_packages` / `plugin_packages` / `externaltool_packages` | 制品 |
| `release_packages` | 发布包元数据 |
| `case_examples` | 案例库 |

Blob 内容落 `MANAGE_BLOB_DIR`（内容寻址文件 + sidecar JSON），不入 SQLite。生产演进：PostgreSQL + S3 / MinIO。

---

## 5. Console（统一运维 UI）

Vue 3 + Vite（`manage/console/frontend/`），构建产物挂载 `/console/`（**不入库**，本地运行或跑单测前须先 build）。

| 导航 | 内容 |
|------|------|
| **Agent 目录** | Registry 列表 / 详情 / discovery 分组 |
| **A2A Inbox** | Task 只读列表 |
| **案例库** | Cases 管理 |
| **Node 配置** | LLM / Skills / Plugins / ExternalTools / 版本发布 子页；内嵌 PageAgent 命令栏 |

默认 **开放模式**：未配置 token 时无需鉴权浏览。

---

## 6. 鉴权模型

| 身份 | 凭据 | 能力 |
|------|------|------|
| **Node** | Header `x-dagents-agent-id`（+ 可选 `x-dagents-a2a-token`） | 注册 / 心跳自己；A2A 以 `from_agent_id` 为界；拉制品 |
| **Member** | member token + groups | Registry 列表（组内）、只读 catalog |
| **Admin** | admin token | 全局目录、制品发布、审计、强制注销 |

未配置 `MANAGE_TOKENS` / `MANAGE_SHARED_TOKEN` 时为开放模式（MVP）。生产 RBAC 待完善。

---

## 7. 进程与配置

**入口**：`python run_manage.py`（默认 `0.0.0.0:8020`）。Docker / 离线 bundle 见 [packaging/manage/](../../packaging/manage/README.md)。

**Manage 环境变量**（`manage/config.py`）

| 变量 | 默认 | 说明 |
|------|------|------|
| `MANAGE_HOST` / `MANAGE_PORT` | `0.0.0.0` / `8020` | 监听 |
| `MANAGE_DB_PATH` | 空=仅内存 | SQLite 路径 |
| `MANAGE_BLOB_DIR` | 空 | Blob 根目录 |
| `MANAGE_BLOB_MAX_BYTES` | 空=不限制 | 单 Blob 上限 |
| `MANAGE_RELEASES_DIR` | `{db 目录}/releases` | 安装包目录 |
| `MANAGE_OFFLINE_GRACE_SECONDS` | `86400` | TTL 过期后 offline 保留 |
| `MANAGE_TOKENS` / `MANAGE_SHARED_TOKEN` | 空 | 启用 Token 模式 |
| `MANAGE_A2A_INBOX_CONTENT_MAX_CHARS` | `4096` | inbox `content` 截断上限 |
| `MANAGE_A2A_EXPIRE_SWEEP_SECONDS` | `30` | TTL 过期扫描间隔 |

**Go Node 配置**（`shared/config`）

```yaml
manage:
  enabled: true
  url: http://127.0.0.1:8020
  registration:
    base_url: http://192.168.1.10:18765   # Console 展示 / 人工跳转
    interval_seconds: 30
    ttl_seconds: 60
  a2a:
    enabled: true
    inbox_wait_seconds: 25
    inbox_poll_seconds: 30
  update:
    channel: stable
```

---

## 8. 协作时序

**启动**

```text
Node 启动（manage.enabled=true）
  → POST /v1/registry/agents        （注册）
  → 后台 heartbeat 循环
  → 后台 inbox 轮询（a2a.enabled）
  → 后台 release 检查（update）
```

**A2A 调用**

```text
Node A  discover → POST /v1/a2a/tasks（+ blob_ids）
Manage  写入 B inbox
Node B  GET inbox → ack → 本地 turn → reply
Node A  GET task → 结果
```

---

## 9. 后续方向

四条主线，**契约先行、Node 主动拉取、非强制推送**（全文见 [manage-phase2-capabilities.md](./manage-phase2-capabilities.md)）：

| 主线 | 要点 |
|------|------|
| **能力市场** | Node 拉 catalog、**选择**安装/移除；`installs` 登记；制品自动同步 |
| **A2A 增强** | `broadcast` 扇出；progress 中继 |
| **审计汇聚** | Node 批量 ingest + Console timeline |
| **复杂 Workflow** | 多 Agent 执行计划：步骤、依赖、验收、汇总 |

其余：LLM / 制品的 Node 自动消费、RBAC 生产化、PostgreSQL / S3 存储后端。

---

**代码入口**：`python run_manage.py` — 详见 [`manage/README.md`](../../manage/README.md)、符号索引 [`manage/REFERENCE.md`](../../manage/REFERENCE.md)。
