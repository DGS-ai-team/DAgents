# Phase 1：Agent Directory / Register Center 企业化（变更草案）

> **状态**：P1.1–P1.5 **已实现**；P1.6 Node sidecar **待做**  
> **对齐**：[roadmap.md](../roadmap.md) §4 Phase 1  
> **现行实现**：`register_center/`（MVP）；契约见 [a2a-and-register-center.md](../a2a-and-register-center.md)

本文定义将 Register Center 从 **MVP 登记服务** 升级为 **企业 Agent 目录** 的数据模型、HTTP API、鉴权与分阶段落地计划。**0.x 阶段允许不兼容调整**，但草案优先采用 **向后兼容的字段扩展**。

---

## 1. 目标与非目标

### 1.1 目标

| 能力 | 说明 |
|------|------|
| **企业登记模型** | 除 `agent_id` / `base_url` / `discovery_group` 外，支持 owner、team、能力标签、工具/skills 摘要、风险与权限范围等可读字段。 |
| **可解释的健康状态** | 在线/离线、最近心跳、版本、可选错误摘要；TTL 过期仍保留「曾登记」语义（见 §4）。 |
| **管理员目录视图** | 全局列表（可不按 `discovery_group` 过滤）、分页、按 team/status 筛选；操作写审计。 |
| **A2A 可观测（RC 侧）** | 在 broadcast/relay 路径记录 trace、耗时、终态；Directory UI 可查询近期调用摘要（完整 inbox 仍属 Manage）。 |

### 1.2 非目标（本 Phase 或 Manage 阶段）

- **Manage 控制面**（A2A inbox、Node 出站仅连 Manage）：见 [future/a2a-via-manage.md](../future/a2a-via-manage.md)。
- **Go Node 内置 `agent_peer` 工具**：不在本 Phase 恢复；Node 可选 **登记心跳 Sidecar**（§7）为独立小步。
- **多副本 HA / PostgreSQL**：Phase 6 与 [roadmap.md](../roadmap.md) §5.2 合并设计；本 Phase 可预留 schema，不强制换存储。
- **LDAP/OIDC**：Phase 6；本 Phase 仅 **API Key 角色**（§6）。

---

## 2. 现状基线（v0.2.17）

### 2.1 登记模型

`AgentUpsertRequest` / `AgentRecord`（`register_center/rc_models.py`）：

| 字段 | 方向 | 说明 |
|------|------|------|
| `agent_id` | 请求 + 响应 | 全局唯一 |
| `base_url` | 请求 + 响应 | Agent HTTP 根地址 |
| `discovery_group` | 请求 + 响应 | 非空字符串列表 |
| `capabilities_hint` | 请求 + 响应 | 可选能力标签 |
| `ttl_seconds` | 请求 | 默认 60，5–3600 |
| `registered_at_unix` | 响应 | upsert 时刻 |
| `expires_at_unix` | 响应 | TTL 截止；过期后 **从 store 删除** |

### 2.2 HTTP 路由

| 方法 | 路径 | 鉴权 | 备注 |
|------|------|------|------|
| `POST` | `/v1/agents` | shared token | upsert = 心跳 |
| `GET` | `/v1/agents` | shared token | **`discovery_group` 必填** |
| `GET` | `/v1/agents/{id}` | shared token | **`discovery_group` 必填** |
| `DELETE` | `/v1/agents/{id}` | shared token | 硬删除 |
| `POST` | `/v1/broadcast` | shared token | 已有 Prometheus 指标 |
| `POST` | `/v1/relay` | shared token | 同上 |

### 2.3 缺口（相对 roadmap Phase 1）

- 无 `name` / `owner` / `team` / `tools` / `skills` / `risk_level` / `allowed_scopes` 等。
- 无显式 `status`；离线 = 记录被 TTL prune，**目录不可见**。
- 无管理员全局列表、分页、审计。
- 无 Directory UI；无 A2A 调用记录查询 API（仅 `/metrics`）。

---

## 3. 目标数据模型

### 3.1 字段一览

**命名约定**：请求体沿用 snake_case；响应中 `base_url` 与 roadmap 的 `endpoint` 同义，**不 rename**（避免破坏现有客户端）。

#### 3.1.1 `AgentUpsertRequest`（扩展）

| 字段 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| `agent_id` | string | 是 | — | 不变 |
| `base_url` | string | 是 | — | 不变 |
| `discovery_group` | string[] | 是 | — | 不变 |
| `capabilities_hint` | string[] | 否 | `[]` | 不变；可与 `capabilities` 合并展示 |
| `ttl_seconds` | int | 否 | 60 | 不变 |
| **`name`** | string | 否 | `agent_id` | 展示名，≤128 |
| **`description`** | string | 否 | `""` | ≤2048 |
| **`owner`** | string | 否 | `""` | 负责人或邮箱，≤256 |
| **`team`** | string | 否 | `""` | 团队/成本中心，≤128 |
| **`capabilities`** | string[] | 否 | `[]` | 结构化能力标签；若空则 UI 回退 `capabilities_hint` |
| **`tools`** | string[] | 否 | `[]` | 工具名摘要（如 `bash_run,read_file`） |
| **`skills`** | string[] | 否 | `[]` | 已加载或可用 skill id |
| **`auth_method`** | enum | 否 | `"shared_token"` | `shared_token` \| `mtls` \| `none`（声明性，RC 不校验对端） |
| **`risk_level`** | enum | 否 | `"medium"` | `low` \| `medium` \| `high` |
| **`allowed_scopes`** | string[] | 否 | `[]` | 权限范围标签，如 `ops:read`, `prod:write` |
| **`version`** | string | 否 | `""` | Agent/Node 版本，如 `0.2.17` |
| **`metadata`** | object | 否 | `{}` | 扩展键值；RC 不解析语义 |

#### 3.1.2 `AgentRecord`（响应）

在 upsert 字段基础上，服务端追加：

| 字段 | 类型 | 说明 |
|------|------|------|
| `registered_at_unix` | int | 首次登记时间（**upsert 不重置**） |
| `updated_at_unix` | int | 最后一次 upsert |
| `last_seen_unix` | int | 同 `updated_at_unix`（心跳时刻） |
| `expires_at_unix` | int | TTL 截止 |
| **`status`** | enum | `online` \| `offline` \| `expired`（见 §4） |
| **`last_error_summary`** | string \| null | 可选；由 heartbeat 或 RC 探测写入 |
| **`recent_task_summary`** | string \| null | 可选；单行摘要，如「session abc 完成 turn」 |

**`first_registered_at` 语义**：首次 upsert 写 `registered_at_unix`；后续 upsert **保留**该值，仅更新 `updated_at_unix` / `last_seen_unix` / `expires_at_unix` 与可变 metadata。

### 3.2 JSON 示例

```json
{
  "agent_id": "ops-node-01",
  "base_url": "http://10.0.1.5:8080",
  "discovery_group": ["ops", "staging"],
  "name": "运维助手 · 机房 A",
  "description": "内网运维诊断与带审批重启",
  "owner": "zhangsan@corp.example",
  "team": "platform-ops",
  "capabilities": ["incident-triage", "log-analysis"],
  "capabilities_hint": ["metrics", "logs"],
  "tools": ["bash_run", "grep_files", "load_skills"],
  "skills": ["service-restart-runbook"],
  "auth_method": "shared_token",
  "risk_level": "high",
  "allowed_scopes": ["ops:read", "staging:write"],
  "version": "0.2.17",
  "ttl_seconds": 120
}
```

---

## 4. 健康状态与 TTL 语义（变更）

### 4.1 问题

现行实现：TTL 到期 **`_prune_expired_locked` 硬删除**，管理员无法看到「曾注册但离线」的 Agent，与 Directory「在线/离线」产品语义冲突。

### 4.2 提议行为

| 状态 | 条件 | 列表默认 | broadcast/relay |
|------|------|----------|-----------------|
| **`online`** | `now < expires_at_unix` | 可见 | 可投递 |
| **`offline`** | `expires_at_unix <= now < expires_at_unix + grace_seconds` | 可见（需 `include_offline=true` 或 admin） | **不可**投递 |
| **`expired`** | 超过 grace | 仅 admin + `include_expired=true` | 不可 |

- **`grace_seconds`**：环境变量 `REGISTER_CENTER_OFFLINE_GRACE_SECONDS`，默认 **86400**（24h）；可设为 `0` 恢复「过期即删」MVP 行为。
- **存储**：仍用单 JSON 文件或内存表；`status` 为 **派生字段**（响应时计算），不落库冗余枚举。
- **可选**：`POST /v1/agents/{id}/heartbeat` 作为 `POST /v1/agents` 的别名（body 可仅含 `ttl_seconds` + 健康字段），减少全量 upsert 带宽。

### 4.3 健康附加字段

Heartbeat / upsert 可选携带：

```json
{
  "version": "0.2.17",
  "last_error_summary": "LLM timeout: chat/completions",
  "recent_task_summary": "session s-42 turn_complete"
}
```

RC **不验证** 内容真实性；Directory 仅展示。后续 Node Sidecar 从 `GET /v1/agent/info` 与本地状态填充。

---

## 5. HTTP API 变更

### 5.1 兼容原则

- 旧客户端仅传 MVP 字段 → 新字段默认空 / 派生 `status=online`。
- `GET /v1/agents?discovery_group=...` **保持必填**（breaking 若改为可选则仅 admin token 可省略）。
- 响应 **只增字段**，不删不改名。

### 5.2 列表查询增强 — `GET /v1/agents`

**Query 参数**：

| 参数 | 必填 | 默认 | 说明 |
|------|------|------|------|
| `discovery_group` | 否* | — | *组员 token **必填**；admin token **可省略** = 全局 |
| `team` | 否 | — | 精确匹配 |
| `status` | 否 | `online` | `online` \| `offline` \| `expired` \| `all` |
| `q` | 否 | — | 模糊匹配 `name` / `agent_id` / `description` |
| `page` | 否 | 1 | ≥1 |
| `page_size` | 否 | 50 | 1–200 |

**响应**：

```json
{
  "agents": [ "..." ],
  "page": 1,
  "page_size": 50,
  "total": 123
}
```

### 5.3 管理员路由 — `GET /v1/admin/agents`

与 §5.2 等价，但 **强制 admin 角色**；便于网关单独挂载策略。若实现成本敏感，可合并为单路由 + 角色分支。

### 5.4 审计 — `GET /v1/admin/audit`

| 字段 | 说明 |
|------|------|
| `at_unix` | 事件时间 |
| `actor` | token id 或 `service:xxx` |
| `action` | `agent.upsert` \| `agent.delete` \| `broadcast` \| `relay` |
| `target_agent_id` | 可选 |
| `discovery_group` | 可选 |
| `detail` | JSON 摘要 |

- 环形缓冲或 JSONL 文件：`REGISTER_CENTER_AUDIT_PATH`。
- Phase 1 **不要求** 独立 DB。

### 5.5 A2A 可观测 — `GET /v1/admin/a2a/recent`

查询近期 broadcast/relay 摘要（与 Prometheus 互补，供 UI 表格）：

| 字段 | 说明 |
|------|------|
| `trace_id` | UUID；请求头 `X-DAgents-Trace-Id` 透传，缺失则 RC 生成 |
| `operation` | `broadcast` \| `relay` |
| `delivery_mode` | `direct` \| `relay`（relay 固定为 `relay`） |
| `caller_groups` | string[] |
| `target_agent_id` | relay 必填 |
| `target_session_id` | 从 payload 提取，可选 |
| `started_at_unix` / `finished_at_unix` | |
| `latency_ms` | |
| `final_state` | `accepted` \| `failed` \| `partial` \| `no_targets` |
| `error_summary` | 可选 |

- 保留现有 `metrics.py`；内存保留最近 **N=500** 条（可配置）。
- **Manage 阶段** 再统一 inbox 级 trace；本 API 仅 RC 侧 hop。

### 5.6 `relay` / `broadcast` 行为

- 投递前校验 target **`status == online`**；否则 `409 agent_offline`。
- 响应头回写 `X-DAgents-Trace-Id`。
- 写入 §5.5  ring buffer + 审计 `a2a.relay` / `a2a.broadcast`。

---

## 6. 鉴权与多租户边界

### 6.1 Token 模型（Phase 1 最小可行）

环境变量（示例）：

```bash
# 现有
REGISTER_CENTER_SHARED_TOKEN=...

# 新增（JSON 或第二文件）
REGISTER_CENTER_TOKENS='[
  {"id":"ops-agent","role":"member","discovery_groups":["ops"]},
  {"id":"admin-console","role":"admin","discovery_groups":["*"]}
]'
```

| 角色 | `POST /v1/agents` | `GET /v1/agents` | `GET /v1/admin/*` | broadcast/relay |
|------|-------------------|------------------|-------------------|-----------------|
| **member** | 仅所属 group | 必须带所属 `discovery_group` | 403 | 仅所属 group 内 target |
| **admin** | 任意 | 可全局、可过滤 | 允许 | 任意（仍写审计） |

- 未配置 `REGISTER_CENTER_TOKENS` 时：**回退** 现有单 token = admin（兼容开发环境）。
- Header 不变：`Authorization: Bearer ...` 或现有 RC header 约定（与 `rc_app.py` 对齐）。

### 6.2 多租户

- **租户边界 = `discovery_group` 列表**；admin 跨组可见。
- `team` 为 **展示/筛选** 维度，不作硬隔离（Phase 6 可与 OIDC claim 绑定）。

---

## 7. Node / Manage 集成（分步）

| 步骤 | 组件 | 内容 |
|------|------|------|
| **P1.2** | 外部登记客户端 | 文档 + 示例脚本：读 `config.yaml` → `POST /v1/agents` |
| **P1.3** | Go Node（可选） | `manage.registration`：`enabled` + `url` + `interval_seconds`；从 `GET /v1/agent/info` 填充 `tools`/`version` |
| **P1.4** | Manage | 登记与 A2A 中枢；RC 可降级为「只读目录副本」或合并进 Manage DB |

**约束**（与 [three-component-model.md](./three-component-model.md) 一致）：Node **仍不** 实现 `agent_peer`；跨 Agent 调用经 Manage。

---

## 8. Agent Directory UI（P1.5）

**首版范围**（静态 SPA 或 Server-rendered，挂载 RC 同源 `/ui/`）：

- 表格：name、team、status、version、last_seen、risk_level、capabilities、tools、skills。
- 筛选：team、status、discovery_group（admin）。
- 详情抽屉：完整字段 + 近期 A2A 记录（§5.5）。
- **调用入口**：仅 deep link 到 `base_url` 文档或预留「Manage 发起 A2A」按钮（disabled 直至 Manage）。

技术选型不在本草案锁定；优先 **只读** + admin token 注入（或反向代理 SSO）。

---

## 9. 存储与迁移

### 9.1 JSON 持久化格式 v2

```json
{
  "schema_version": 2,
  "agents": [ { "... AgentRecord fields ..." } ]
}
```

- 启动时：`schema_version` 缺失 → 视为 v1，补默认新字段。
- v1 → v2：**非破坏性**；`registered_at_unix` 保留；首次加载写 `updated_at_unix = registered_at_unix`。

### 9.2 单测要求

- 模型校验（新 enum、长度）。
- TTL + grace → `status` 派生。
- member/admin 列表与 403。
- relay 对 offline agent 返回 409。
- audit ring 与 a2a recent 上限。

---

## 10. 实施里程碑

| ID | 交付 | 依赖 | 验收 |
|----|------|------|------|
| **P1.1** | 扩展 `rc_models` + store v2 + 派生 status | — | 旧客户端 upsert/list 仍绿；新字段 round-trip |
| **P1.2** | 列表分页/筛选 + admin 全局视图 + token 角色 | P1.1 | member 不能跨组；admin 可 `total` 分页 |
| **P1.3** | audit JSONL + a2a recent + trace 头 | P1.1 | UI/API 能查最近 relay；metrics 仍可用 |
| **P1.4** | 登记示例 + `register_center` README/REFERENCE | P1.1 | 文档与 OpenAPI 对齐 |
| **P1.5** | Directory UI 只读 | P1.2, P1.3 | 浏览器可见 online/offline 列表 |
| **P1.6** | Node 可选 registration sidecar | P1.1, config | `manage.registration.enabled` 周期性 upsert |

**建议 PR 顺序**：P1.1 → P1.2 → P1.3（可并行 UI mock）→ P1.4 文档 → P1.5 → P1.6。

---

## 11. 开放问题

1. **`capabilities` vs `capabilities_hint`**：是否 deprecate hint，还是长期双字段（UI 合并）？
2. **grace 默认 24h**：是否与合规「立即不可见」冲突？是否按部署 profile 区分？
3. **Directory UI 部署**：RC 内置静态文件 vs 独立 `dagents-console` 包？
4. **与 Manage 合并时间表**：RC 独立演进 vs Manage 一次到位 — 需 [three-component-model.md](./three-component-model.md) ADR 补充一页。

---

## 12. 文档与代码同步清单

实现各里程碑时需更新：

| 文件 | 内容 |
|------|------|
| `register_center/rc_models.py` | 模型 |
| `register_center/rc_store.py` | v2 持久化、grace、first_seen |
| `register_center/rc_app.py` | 路由、鉴权、audit |
| `register_center/README.md` / `REFERENCE.md` | 路由与 env |
| `docs/a2a-and-register-center.md` | §2 路由表 |
| `docs/roadmap.md` | Phase 1 进度 |
| `CHANGELOG.md` | 每里程碑条目 |

---

**草案版本**：2026-06-10 — 对齐仓库 **v0.2.17** 与 roadmap Phase 1 缺口分析。
