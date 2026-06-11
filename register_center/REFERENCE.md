# register_center / REFERENCE

## `rc_models.py`

### 类型别名

- `AuthMethod` — `shared_token` \| `mtls` \| `none`
- `RiskLevel` — `low` \| `medium` \| `high`

### 类

- `AgentUpsertRequest` — 登记/心跳请求（MVP + Phase 1 扩展字段）
- `AgentStoredRecord` — 持久化层记录（无 `status`）
- `AgentRecord` — 对外响应（含派生 `status`）
- `AgentListResponse` — `agents` + `page` / `page_size` / `total`
- `AuditListResponse` / `A2ARecentListResponse` — admin 查询壳
- `HealthResponse` / `Broadcast*` / `Relay*` — 不变契约，见源码

## `rc_store.py`

- `AgentListQuery` — 列表筛选（group/team/status/q/分页）
- `stored_to_public(record) -> AgentRecord` — 附加派生 status
- `AgentRegistryStore`
  - `upsert` / `get` / `list(query)` / `list_deliverable` / `delete` / `count`
  - v2 JSON 持久化；grace 后 prune `expired`

## `rc_status.py`

- `offline_grace_seconds() -> int`
- `derive_status(...) -> AgentStatus`
- `is_deliverable(...) -> bool`

## `rc_auth.py`

- `AuthContext` — `token_id` / `role` / `discovery_groups`
- `authenticate(request) -> AuthContext`
- `require_admin(auth) -> None`

## `rc_audit.py`

- `AuditEvent` / `AuditLog` — `record` / `list_recent`

## `rc_a2a_recent.py`

- `A2ARecentEntry` / `A2ARecentLog` — `record` / `list_recent`

## `rc_app.py`

- `create_app() -> FastAPI` — 注册 agents/admin/broadcast/relay 路由；挂载 **`/ui/`** Directory UI
- `app` — 默认应用实例

## `ui/`

- `index.html` / `styles.css` / `app.js` — Agent Directory 只读前端（同源调用 `/v1/*`）

## `metrics.py`

- Prometheus A2A 指标 helpers
