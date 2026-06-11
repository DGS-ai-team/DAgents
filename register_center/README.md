# register_center

Register Center 的实现目录（FastAPI + 默认内存存储，可选 JSON 文件持久化；**Phase 1** 企业目录字段与 admin API）。

## 目录说明

| 路径 | 说明 |
|------|------|
| `__init__.py` | 包标记文件。 |
| `rc_models.py` | 请求/响应 Pydantic 模型与字段校验规则。 |
| `rc_store.py` | 登记表存储（v2 JSON、grace TTL、分页筛选）。 |
| `rc_status.py` | 在线/离线/过期状态派生。 |
| `rc_auth.py` | shared token + `REGISTER_CENTER_TOKENS` 角色鉴权。 |
| `rc_audit.py` | 审计环形缓冲 + 可选 JSONL。 |
| `rc_a2a_recent.py` | broadcast/relay 近期摘要环形缓冲。 |
| `rc_app.py` | FastAPI 应用工厂与 REST 路由定义。 |
| `metrics.py` | Prometheus A2A 指标（relay/broadcast） |
| `ui/` | Agent Directory 只读 Web UI（`/ui/`） |
| `REFERENCE.md` | 本目录 Python 符号索引。 |

## 运行方式

在仓库根目录执行：

```bash
python run_register_center.py
```

默认监听 `0.0.0.0:8010`，可通过环境变量覆盖：

| 变量 | 说明 |
|------|------|
| `REGISTER_CENTER_HOST` / `REGISTER_CENTER_PORT` | 监听地址 |
| `REGISTER_CENTER_STORE_PATH` | 可选 JSON 存储；启用后 upsert/delete/prune 原子写回 |
| `REGISTER_CENTER_OFFLINE_GRACE_SECONDS` | TTL 过期后保留为 `offline` 的秒数，默认 `86400`；`0` 恢复 MVP「过期即删」 |
| `AGENT_PEER_SHARED_TOKEN` | 未配置 `REGISTER_CENTER_TOKENS` 时的 shared token；匹配则 **admin** |
| `REGISTER_CENTER_TOKENS` | JSON 数组：`{"id","token","role":"member\|admin","discovery_groups":[...]}` |
| `REGISTER_CENTER_AUDIT_PATH` | 可选审计 JSONL 追加路径 |
| `REGISTER_CENTER_AUDIT_MAX_ENTRIES` | 内存审计条数上限，默认 `500` |
| `REGISTER_CENTER_A2A_RECENT_MAX_ENTRIES` | A2A 摘要条数上限，默认 `500` |

## 核心接口

### 系统

- `GET /metrics` — Prometheus 指标
- `GET /health` — 健康与登记计数
- **`GET /ui/`** — Agent Directory 只读 Web UI（token 存 sessionStorage；支持 `?token=`）

### Agent 目录

- `POST /v1/agents` — 登记/心跳（扩展字段见 [agent-directory-phase1.md](../docs/design/agent-directory-phase1.md)）
- `GET /v1/agents` — 列表（`discovery_group` member 必填 / admin 可省略；`status`/`team`/`q`/分页）
- `GET /v1/agents/{agent_id}` — 单条（`discovery_group` 必填）
- `DELETE /v1/agents/{agent_id}` — 注销

### Admin（需 admin token）

- `GET /v1/admin/audit?limit=` — 近期审计
- `GET /v1/admin/a2a/recent?limit=` — 近期 broadcast/relay 摘要

### A2A

- `POST /v1/broadcast` — 向 **online** Agent 广播（响应头 `X-DAgents-Trace-Id`）
- `POST /v1/relay` — 中继到 **online** Agent；离线返回 `409`

鉴权 Header：`x-dagents-a2a-token` 或 `Authorization: Bearer …`。

## 设计文档

- [docs/design/agent-directory-phase1.md](../docs/design/agent-directory-phase1.md) — Phase 1 变更草案与里程碑（**P1.5 UI 已内置**；P1.6 Node sidecar 待做）
