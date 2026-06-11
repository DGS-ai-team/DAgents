# Manage 统一控制面架构（方案）

> **状态**：方案（未实现）  
> **取代**：独立 `register_center/` 服务及其 **Node 直连 relay/broadcast** 模型  
> **对齐**：[three-component-model.md](./three-component-model.md)、[roadmap.md](../roadmap.md) Phase 1–4

---

## 1. 定位

**Manage** 是 DAgents 的 **唯一 Python 控制面服务**：管理所有注册到 Manage 的 **Agent Node**，不参与 Client 会话与 LLM turn loop。

| 原则 | 说明 |
|------|------|
| **Node 仅出站连 Manage** | 注册、心跳、A2A 收信、skill 同步、审计上报均为 **Node → Manage** |
| **禁止 Node-to-Node** | 跨 Agent 消息与文件 **经 Manage 中继**；discover API **不返回** peer 路由 URL |
| **Client 不连 Manage** | 运维人员通过 **Manage Console（Web）** 或 admin API 查看全局状态 |
| **企业可治理** | 接入、协作、skill 发布均带 **身份、审批、审计** |
| **本地优先** | 默认 SQLite + 本地 blob 目录；可选 PostgreSQL / S3 兼容存储 |

一句话：

> Manage = **Agent 接入目录** + **A2A 协作总线** + **Skill 分发与审批** + **审计与运维控制台**。

---

## 2. 与旧 Register Center 的关系

### 2.1 为何要移除独立 RC

现行 `register_center/` 存在结构性偏差：

| 问题 | 说明 |
|------|------|
| **A2A 模型错误** | `/v1/relay`、`/v1/broadcast` **直连** Agent `base_url`，违反「禁止 Node-to-Node」 |
| **职责过窄** | 只有目录 + 中继，无法承载 skill 生命周期、统一审计、企业控制台 |
| **双服务运维** | RC 与未来 Manage 并存会增加配置、token、存储重复 |
| **Node 未集成** | Go Node `manage.enabled: false`，目录数据靠外部脚本写入 |

### 2.2 迁移策略（方案级）

| 阶段 | 动作 |
|------|------|
| **M0** | 新建 `manage/`，实现 Registry 模块（吸收 RC Phase 1 能力） |
| **M1** | Node `manage.url` 注册 + 心跳；废弃「手动 POST RC」 |
| **M2** | A2A inbox 上线；**标记 RC relay/broadcast 为 deprecated** |
| **M3** | Skill Registry 上线；Console 合并 Directory UI |
| **M4** | 删除 `register_center/`、`run_register_center.py`、打包入口 `dagents_register_center` |

**兼容窗口**：Manage 可提供 **Legacy Adapter**（可选环境变量 `MANAGE_LEGACY_DIRECT_RELAY=1`）映射旧 relay 语义到 inbox，仅用于过渡，默认关闭。

---

## 3. 总体架构

```text
                         ┌─────────────────────────────────────────┐
                         │              Manage Service              │
                         │  (Python FastAPI, 默认 :8020)            │
                         ├─────────────────────────────────────────┤
  Admin Browser ────────►│  Console (Web)     /console/*          │
                         │  OpenAPI           /docs                 │
                         ├──────────┬──────────┬──────────┬─────────┤
                         │ Registry │   A2A    │  Skills  │ Platform│
                         │  接入目录 │ 协作总线  │ 分发中心  │ 横切能力 │
                         ├──────────┴──────────┴──────────┴─────────┤
                         │  Storage: metadata DB + Blob Store       │
                         └──────────────────▲───────────────────────┘
                                            │ HTTPS 出站（Node Token）
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

### 3.1 四大域 + 横切平台

| 域 | 代号 | 职责 |
|----|------|------|
| **① Registry** | `registry` | Agent Node 接入、心跳、目录、discover、分组与可见性 |
| **② A2A** | `a2a` | Node 间消息、文件/制品传递、会话级协作状态 |
| **③ Skills** | `skills` | Skill 包注册、审批、版本、签名分发与 Node 同步 |
| **④ Platform** | `platform` | 鉴权、审计、Blob、Console、配置、可观测 |

用户提出的三块对应 **①②③**；**④** 为建议扩展（见 §5）。

---

## 4. 模块详细设计

### 4.1 Registry（接入与目录）— 取代 RC 核心

**职责**

- Node **启动注册**、**周期心跳**、**优雅注销**
- 维护 Agent 身份与能力清单（对齐 Phase 1 扩展字段）
- **Discover**：供 A2A 工具查询可协作 Agent（`expose_to_peers=true` 且 online）
- **Admin 目录**：全局列表、筛选、Directory UI（自 RC `/ui/` 迁入 Console）

**关键字段**

| 字段 | 说明 |
|------|------|
| `agent_id` | 全局唯一；一进程一 id |
| `groups[]` | 发现分组（原 `discovery_group`） |
| `expose_to_peers` | 是否可作为 A2A 目标 |
| `capabilities[]` / `tools[]` | 能力摘要（心跳刷新） |
| `node_version` / `host_info` | 版本与主机快照 |
| `owner` / `team` / `risk_level` | 治理维度 |
| `status` | online / offline / expired（grace TTL） |
| `endpoint` | **仅运维探活展示**；**禁止**用于 A2A 路由 |

**API 前缀**：`/v1/registry/...`（或兼容 `/v1/agents/...` 别名一层）

| 方法 | 路径 | 调用方 |
|------|------|--------|
| POST | `/v1/registry/agents` | Node 注册/更新（upsert） |
| POST | `/v1/registry/agents/{id}/heartbeat` | Node 心跳 |
| POST | `/v1/registry/agents/{id}/deregister` | Node 停机 |
| GET | `/v1/registry/agents` | Admin / member 列表 |
| GET | `/v1/registry/agents/discover` | Node A2A 发现 |
| GET | `/v1/registry/agents/{id}` | 详情 |
| DELETE | `/v1/registry/agents/{id}` | Admin 强制注销 |

**存储**：`registry_agents` 表；TTL + offline grace（沿用 RC Phase 1 语义）。

---

### 4.2 A2A（协作总线）— Task + Inbox

**设计原则**

- **Task + Inbox 模型**：Manage 持久化工作单元；被调方 **long poll inbox**（与 [a2a-via-manage.md](../future/a2a-via-manage.md) 一致）
- **无 peer URL**：调用方只持有 `to_agent_id`
- **不做 messages 兼容**：API 统一 `/v1/a2a/tasks`，无 `/v1/a2a/messages`
- **大 payload 走 Blob**：正文、文件引用 `blob_ids`

#### 4.2.1 Task API（M2）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/a2a/tasks` | 创建 Task（`kind`: invoke \| notify） |
| GET | `/v1/a2a/inbox` | 被调方 long poll（`?wait=25s`） |
| POST | `/v1/a2a/tasks/{id}/ack` | 标记 processing |
| POST | `/v1/a2a/tasks/{id}/reply` | 回复（可含 blob） |
| GET | `/v1/a2a/tasks/{id}` | 调用方查状态/结果 |
| POST | `/v1/a2a/broadcast` | 按 groups 扇出（Phase 2） |

**Task 体（摘要）**

```json
{
  "task_id": "a2a-task-...",
  "from_agent_id": "A",
  "to_agent_id": "B",
  "kind": "invoke",
  "content": "简短文本",
  "blob_ids": ["blob-..."],
  "caller_session_id": "sess-a",
  "idempotency_key": "...",
  "ttl_seconds": 3600,
  "trace_id": "..."
}
```

**校验**：`to` 存在、online、`expose_to_peers=true`；caller Header `x-dagents-agent-id` 与 `from_agent_id` 一致。

#### 4.2.2 文件 / 制品传输（Blob）

统一 **Platform Blob API**（A2A 与 Skills 共用）：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/blobs` | 上传（multipart）；返回 `blob_id`、sha256、size |
| GET | `/v1/blobs/{id}` | 下载（Node token 或 scoped URL） |
| HEAD | `/v1/blobs/{id}` | 元数据 |
| DELETE | `/v1/blobs/{id}` | Admin 或 TTL 回收 |

**A2A 文件流**

```text
Node A  POST /v1/blobs        → blob_id
        POST /v1/a2a/tasks    → { blob_ids: [blob_id], to: B }
Node B  GET  /v1/a2a/inbox    → 见 task
        GET  /v1/blobs/{id}   → 落盘
        POST /v1/a2a/tasks/{id}/reply
```

#### 4.2.3 Node 侧（M2 起）

Go Node：

- **`manage/a2a` inbox poller** — long poll + 断线短 poll（`node/internal/manage/inbox_poller.go`）
- 工具（待接 turn loop）：`agent_discover`、`agent_invoke`、`agent_notify`

**禁止**恢复直连 `base_url` 的 `agent_peer` 旧路径。

---

### 4.3 Skills（分发中心）

**职责**

- 企业 **Skill 包** 作为版本化制品（`SKILL.md` + 可选 `scripts/`、`references/`）
- **审批工作流**：draft → pending_review → approved → published → deprecated
- **仅 published** 且 Node **有权**（team / scope）的可下载
- Node **同步清单**：心跳返回 `skills_catalog_version`；有更新则拉取

#### 4.3.1 Skill 包模型

| 字段 | 说明 |
|------|------|
| `skill_id` | 稳定 ID（如 `service-restart-runbook`） |
| `version` | semver（`1.2.0`） |
| `name` / `description` | 与 SKILL frontmatter 一致 |
| `owner` / `team` | 归属 |
| `risk_level` | low / medium / high |
| `required_tools[]` | 声明依赖工具 |
| `required_scopes[]` | Node 必须具备的 scope |
| `blob_id` | 打包产物（zip） |
| `status` | 生命周期状态 |
| `approval` | 审批记录引用 |

#### 4.3.2 API

| 方法 | 路径 | 调用方 |
|------|------|--------|
| POST | `/v1/skills/packages` | 上传新版本（developer） |
| POST | `/v1/skills/packages/{id}/submit` | 提交审批 |
| POST | `/v1/skills/packages/{id}/approve` | 审批通过（admin/approver） |
| POST | `/v1/skills/packages/{id}/reject` | 拒绝 |
| POST | `/v1/skills/packages/{id}/publish` | 发布 |
| GET | `/v1/skills/catalog` | Node 或 Console 列表（已发布） |
| GET | `/v1/skills/catalog/{skill_id}` | 元数据 + 最新/指定版本 |
| GET | `/v1/skills/catalog/{skill_id}/versions/{ver}/download` | Node 下载 zip |
| GET | `/v1/skills/sync/manifest` | Node 增量同步（ETag / version） |

#### 4.3.3 Node 同步行为

```text
heartbeat 响应 → { skills_catalog_version: 42 }
Node  GET /v1/skills/sync/manifest?since=41
      → [{ skill_id, version, sha256, download_url }]
Node  下载 → 校验 sha256 → 解压到 {fs_root}/skills/{skill_id}/
      → 现有 Catalog 扫描 SKILL.md（无需改格式）
```

**与本地 skill 关系**

- **本地自建** skill（`.runtime/skills/` 手工添加）保留
- **Manage 下发** skill 带 `source: manage` 标记；可选禁止 Node 覆盖已发布版本

**审批**

- 首版：Console 按钮 + admin API；与 Node HITL **分离**（skill 发布是企业资产审批）
- Phase 2：对接外部工单 / SSO 审批人

---

### 4.4 Platform（横切能力）— 建议扩展

除你提出的三块外，建议 Manage **统一承载** 以下能力，避免再拆服务：

| 子模块 | 说明 | 优先级 |
|--------|------|--------|
| **Auth & Tenancy** | Node token（每 agent 或预共享）、admin token、member groups、`team`/`org` 边界 | P0 |
| **Audit Hub** | Node 批量上报 turn/tool/A2A/skill 事件；Console 检索与导出 | P0 |
| **Blob Store** | A2A 文件 + Skill 包 + 附件共用 | P0 |
| **Console** | 统一 Web：`/console/`（Directory + A2A 追踪 + Skills + Audit） | P1 |
| **Observability** | `/metrics`、A2A trace、结构化日志 | P1 |
| **Policy Registry** | 可选：集中 policy 模板下发；**默认仍 Node 本地 policy**，Manage 只审计 | P2 |
| **Webhook / Events** | Agent 上下线、skill 发布、A2A 失败 → 企业 ITSM | P2 |
| **Quota & Rate** | 每 agent blob 配额、A2A 速率、skill 下载带宽 | P2 |

**Audit Hub（补充细节）**

```http
POST /v1/audit/events     ← Node 批量（幂等 event_id）
GET  /v1/audit/events     ← Admin 查询
GET  /v1/audit/timeline   ← 按 session 串联（Phase 2）
```

与 Node 本地 JSONL **并存**：Manage 为 **企业级汇聚**；Node 保留本地回放能力。

---

## 5. 代码目录框架（目标仓库布局）

**移除** `register_center/` 后，新建：

```text
manage/
  README.md
  REFERENCE.md
  manage_app.py          # FastAPI create_app()
  config.py              # 环境变量 / 设置
  storage/
    db.py                # SQLite / PostgreSQL
    blob.py              # 本地目录或 S3
  platform/
    auth.py
    audit.py
    metrics.py
    errors.py
  registry/
    models.py
    store.py
    routes.py            # 吸收 rc_* 逻辑
  a2a/
    models.py
    inbox.py
    routes.py
  skills/
    models.py
    lifecycle.py         # 审批状态机
    routes.py
  console/
    static/              # 统一 Web UI（Directory + Skills + …）
  tests/                 # 或仓库 tests/test_manage_*.py

run_manage.py            # 仓库根入口（取代 run_register_center.py）
```

**进程入口**

```bash
python run_manage.py     # 默认 0.0.0.0:8020
# 或 dagents manage       # 安装包
```

**配置（环境变量）**

| 变量 | 说明 |
|------|------|
| `MANAGE_HOST` / `MANAGE_PORT` | 监听 |
| `MANAGE_DB_PATH` | SQLite 路径 |
| `MANAGE_BLOB_DIR` | Blob 根目录 |
| `MANAGE_NODE_TOKENS` / `MANAGE_ADMIN_TOKENS` | JSON 角色配置 |
| `MANAGE_OFFLINE_GRACE_SECONDS` | Registry TTL grace |

**Go Node 配置（`shared/config`）**

```yaml
manage:
  enabled: true
  url: http://127.0.0.1:8020
  node_token: ${MANAGE_NODE_TOKEN}
  registration:
    interval_seconds: 30
  a2a:
    inbox_poll_seconds: 5
  skills:
    sync_on_heartbeat: true
```

---

## 6. 数据存储

| 数据 | 首版 | 生产演进 |
|------|------|----------|
| Agent 目录 | SQLite 表 `registry_agents` | PostgreSQL |
| A2A inbox | SQLite `a2a_messages` | PostgreSQL + 分区 |
| Skill 元数据 | SQLite `skill_packages` | PostgreSQL |
| Blob 内容 | 本地 `{blob_dir}/{sha256前缀}/` | S3 / MinIO |
| 审计 | SQLite + 可选 JSONL 追加 | ClickHouse / PG |

**不**再使用 RC 单文件 JSON 作为主存储；可提供 **一次性 import** 工具：`manage-import-rc-json`。

---

## 7. 鉴权模型

| 身份 | Token | 能力 |
|------|-------|------|
| **Node** | `Authorization: Bearer <node_token>` | 注册/心跳自己；A2A 以 `from_agent_id` 为界；拉 skill |
| **Member** | member token + groups | Registry 列表（组内）；只读 catalog |
| **Admin** | admin token | 全局目录、审批 skill、audit、强制注销 |
| **Approver** | approver role（可选） | skill approve/reject，无 agent 删除 |

Node token 可 **每 agent 签发**（注册时返回）或 **部署期预配置**（小团队）。

---

## 8. Console（统一运维 UI）

合并 RC `/ui/` 为 Manage Console 子模块：

| 页面 | 内容 |
|------|------|
| **Directory** | Agent 列表/详情（Registry） |
| **A2A** | 近期消息 trace、失败原因 |
| **Skills** |  catalog、审批队列、版本历史 |
| **Audit** | 事件检索（Phase 2 timeline） |

路由：`https://manage.corp/console/`；静态资源同源，token 同 RC UI（sessionStorage）。

---

## 9. 与 Agent Node 的协作时序

### 9.1 启动

```text
Node 启动
  → POST /v1/registry/agents        （注册）
  → GET  /v1/skills/sync/manifest   （可选首次全量）
  → 后台：heartbeat 循环
  → 后台：inbox 轮询
  → 后台：audit 批量 flush
```

### 9.2 A2A 调用

```text
Node A  discover → send message（+ blob）
Manage  写入 B inbox
Node B  inbox → 本地 session turn → reply
Node A  GET message status → 结果
双方    POST audit events
```

---

## 10. 实施里程碑

| ID | 交付 | 说明 |
|----|------|------|
| **M0** | `manage/` 骨架 + Platform（auth、blob、health） | 可运行空服务 |
| **M1** | Registry 模块 | 端口 RC Phase 1；import 测试；Node registration sidecar |
| **M2** | A2A inbox + blob | 废弃 direct relay；Node inbox 轮询 |
| **M3** | Skills 生命周期 + 下载 | Console Skills 页 |
| **M4** | Audit ingest + Console 合并 | Directory 迁入 |
| **M5** | 删除 `register_center/` | 文档/打包/CI 切换；CHANGELOG breaking |

**建议 PR 切分**：M0+M1 → M2 → M3 → M4 → M5（最后一 PR 删 RC）。

---

## 11. 文档与打包变更清单

| 项 | 动作 |
|----|------|
| `register_center/` | **删除**（M5） |
| `run_register_center.py` | → `run_manage.py` |
| `packaging/.../dagents_register_center` | → `dagents-manage` |
| `docs/a2a-and-register-center.md` | 重写为 `docs/manage.md` |
| `docs/design/agent-directory-phase1.md` | 标注 superseded by 本文 Registry 章 |
| `docs/future/manage-api-sketch.md` | 合并入本文或改为索引 |
| `shared/config` `manage.*` | 扩展 registration/a2a/skills |
| `CHANGELOG.md` | M5 记 breaking：RC 移除 |

---

## 12. 开放决策（已确认 2026-06-10）

| # | 决策 |
|---|------|
| 1 | **端口可配置**：`MANAGE_HOST` / `MANAGE_PORT`，默认 **8020** |
| 2 | **Skill 包**：**一 zip 一 skill**（M3） |
| 3 | **单文件上限**：`MANAGE_BLOB_MAX_BYTES` 可配置；**未设置则不限制**（待运营数据后再定默认值） |
| 4 | **Policy**：首版 **不上 Manage**，Node 本地 policy 不变 |
| 5 | **Legacy relay**：`MANAGE_LEGACY_DIRECT_RELAY` 默认 **关闭**；M2 可选适配 RC 直连语义 |

---

## 13. 实施进度

| ID | 状态 | 交付 |
|----|------|------|
| **M0** | **已完成** | `manage/` 骨架、Platform（auth/audit/blob/metrics）、`run_manage.py` |
| **M1** | **已完成** | Registry SQLite、`/v1/registry/*`、discover、测试 |
| **M2** | 待做 | A2A inbox + Blob API |
| **M3** | 待做 | Skills 生命周期 + 下载 |
| **M4** | 部分 | Console Registry 页已上线；Audit timeline / Skills 页待做 |
| **M5** | 待做 | 删除 `register_center/` |

---

**方案版本**：2026-06-10 — M0+M1 已落地于 `manage/`。

| 你的模块 | Manage 域 | 要点 |
|----------|-----------|------|
| RC 注册中心 | **Registry** | 已 M1：`/v1/registry/*`；**去掉** peer 直连 relay（M2 后） |
| A2A 通信 | **A2A + Blob** | M2：inbox 消息；文件走 Blob 引用 |
| Skills 分发 | **Skills** | M3：一 zip 一 skill；审批 → 发布 → Node 同步 |

| 建议扩展 | Platform |
|----------|----------|
| 审计汇聚、统一 Console、鉴权多租户、Webhook、配额 | M0 已含 auth/audit/blob 占位 |

**代码入口**：`python run_manage.py` — 见 [`manage/README.md`](../../manage/README.md)。
