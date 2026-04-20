# Register 中心设计说明

## 0. 文档信息

| 项 | 说明 |
|----|------|
| **目标读者** | 实现 `register-center/` 服务、以及在 DAgents 中对接注册发现的开发者 |
| **与 A2A 关系** | 对齐 **Registries / Catalogs** 的「目录」职责；**不**实现 A2A JSON-RPC 任务协议本身 |
| **MVP 范围** | 单进程、内存存储、无鉴权；可独立部署，与 **`app/`** 内 Agent 服务通过 HTTP 解耦 |

---

## 1. 定位

**Register 中心**（计划实现于仓库根目录 **`register-center/`**）承担 **策展型 Agent 目录** 角色：

- 为客户端提供 **「逻辑 `agent_id` → 可连接的 `base_url`」** 的映射；
- 客户端拿到 `base_url` 后，自行请求 **`{base_url}/.well-known/agent-card.json`**（或你们约定的 Well-Known 路径），**本服务不代理、不缓存 Card**（MVP）；
- **不替代**各 Agent 进程内对模型、工具、会话的实现（仍在 **`app/`**）。

---

## 2. 职责边界

| 负责 | 不负责 |
|------|--------|
| 登记 / 更新 /（可选）注销 `agent_id` → `base_url` | 转发或解析 A2A 业务 JSON-RPC |
| 列表、按 ID 查询、按 `discovery_group` 筛选 | 审批流、多租户计费（MVP 不做） |
| **`GET /health`**（存活 + 当前登记数） | 多副本间强一致同步（MVP 单进程内存） |
| （可选）**心跳续期** 设计占位 | 替 Agent 做 TLS 终止（由网关/Agent 各自负责） |

---

## 3. 数据模型（MVP）

### 3.1 字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| **agent_id** | `string` | 是 | 集群内唯一；`POST` 重复则 **覆盖**（last-write-wins） |
| **base_url** | `string` | 是 | Agent HTTP 根地址；**无**尾部 `/`（服务端规范化） |
| **discovery_group** | `string \| string[]` | 是 | 发现分组（支持多组）；查询时按“成员关系”命中 |
| **capabilities_hint** | `string[]` | 否 | 能力提示标签，用于目录发现阶段初筛 |
| **registered_at_unix** | `number` | 服务端填 | 最近一次 **成功登记** 的 Unix 秒时间戳 |

### 3.2 约束与规范化

- **agent_id**：建议 `[a-zA-Z0-9._:-]+`，长度上限（如 256）在实现时校验；
- **base_url**：必须是 `http` 或 `https` 的绝对 URL；实现时 `rstrip('/')` 再存储；
- **`discovery_group`**：支持字符串或字符串列表；入库统一为字符串列表并去重；
- **`capabilities_hint`**：可选，入库时去空白/去重。

---

## 4. API 约定（MVP）

### 4.1 一览

| 方法 | 路径 | 说明 |
|------|------|------|
| **POST** | `/v1/agents` | 登记或全量更新一条记录 |
| **GET** | `/v1/agents` | 列表；**必须**携带 **`?discovery_group=`**，不开放全量查询 |
| **GET** | `/v1/agents/{agent_id}` | 单条；需带 `?discovery_group=`，不存在或跨组均 **404** |
| **DELETE** | `/v1/agents/{agent_id}` | 注销指定 Agent 记录 |
| **GET** | `/health` | `{"status":"ok","agents": <count>}` 或等价结构 |
| **POST** | `/v1/broadcast` | 按分组列表广播消息到已注册 Agent |
| **POST** | `/v1/relay` | 按 `target_agent_id` 中继单条消息到目标 Agent |

### 4.2 POST `/v1/agents` 请求体（示例）

```json
{
  "agent_id": "dagents-demo-1",
  "base_url": "https://agent.example.com:8443",
  "discovery_group": ["lab-east", "lab-canary"],
  "capabilities_hint": ["code", "review"]
}
```

### 4.3 POST 响应（示例）

```json
{
  "agent_id": "dagents-demo-1",
  "base_url": "https://agent.example.com:8443",
  "discovery_group": ["lab-east", "lab-canary"],
  "capabilities_hint": ["code", "review"],
  "registered_at_unix": 1710000000
}
```

### 4.4 GET `/v1/agents` 响应（示例）

```json
{
  "agents": [
    {
      "agent_id": "dagents-demo-1",
      "base_url": "https://agent.example.com:8443",
      "discovery_group": ["lab-east", "lab-canary"],
      "capabilities_hint": ["code", "review"],
      "registered_at_unix": 1710000000
    }
  ]
}
```

- **`?discovery_group=lab-east`**：仅返回 `discovery_group` 列表中包含 `lab-east` 的项；未传 query 将返回 422（禁止全量视图）。

### 4.5 错误语义

| HTTP | 场景 |
|------|------|
| **422** | 缺少字段、`base_url` 非法 scheme、**`agent_id` 为空** 等 |
| **404** | `GET /v1/agents/{agent_id}` 指定 `agent_id` 不存在或不在当前分组可见范围 |
| **500** | 未预期内部错误 |

---

## 5. 架构（建议实现形态）

```
┌─────────────┐      POST/GET            ┌──────────────────┐
│ Agent 进程   │ ───────────────────────► │ register-center  │
│ (DAgents     │     /v1/agents           │ FastAPI + 内存    │
│  run_agent_api)│                         │ dict[agent_id]   │
└─────────────┘                           └────────┬─────────┘
        ▲                                            │
        │ GET /.well-known/agent-card.json           │ GET /v1/agents
        │                                            ▼
┌─────────────┐                           ┌──────────────────┐
│  客户端      │ ◄─────────────────────────│  先查目录再连 Agent │
└─────────────┘                           └──────────────────┘
```

- **存储**：`dict[str, AgentRecord]` + 异步锁或同步锁；无持久化；
- **进程模型**：与 **`run_agent_api.py`** 类似，独立 **`uvicorn`** 入口（如 **`register-center/run.py`** 或根目录 **`run_register_center.py`**）；
- **配置**：**`HOST`/`PORT`**、（后续）**`REGISTRY_ADMIN_TOKEN`**。

---

## 6. 与 DAgents 仓库的衔接

| 变量 / 约定 | 说明 |
|-------------|------|
| **`REGISTRY_URL`** | Register 中心对外 base（如 `http://127.0.0.1:8090`）；Agent 启动时若设置则 **POST** 自身 **`AGENT_PUBLIC_BASE_URL`**（或等价） |
| **`DISCOVERY_GROUP`** | Agent 登记时写入 **`discovery_group`**；客户端列表筛选与之一致 |
| **`AGENT_PUBLIC_BASE_URL`** | Agent 对外可被访问的 URL（供写入 **`base_url`**）；开发环境常为 `http://127.0.0.1:8000` |

**自登记（可选实现步骤）**：在 **`app/harness/api`** 的 **lifespan** `startup` 中 `httpx.post(REGISTRY_URL/v1/agents, ...)`，`shutdown` 中 **`DELETE`** 或依赖租约过期（后续）。

---

## 7. 发现流程（客户端）

1. **`GET {REGISTRY_URL}/v1/agents?discovery_group=...`**（或不要筛选则省略 query）；
2. 对目标 **`base_url`** 拉取 **Agent Card**（路径以 A2A/你们约定为准）；
3. 再按 A2A 或现有 HTTP 协议连接 Agent 执行任务。

---

## 8. 演进方向（未实现）

| 方向 | 说明 |
|------|------|
| **持久化** | Redis / PostgreSQL；多实例 + 前置负载均衡 |
| **心跳与租约** | `POST /v1/agents/{id}/heartbeat`；超时自动剔除；防僵尸登记 |
| **策展 / 审批** | `pending` → `approved`；默认列表只出已批准 |
| **索引维度** | `tags[]`、`skill_ids[]`、`tenant_id`；与 **`discovery_group`** 组合筛选 |
| **缓存 Agent Card** | 登记时上报 Card JSON；需版本号与失效策略 |
| **安全** | mTLS、API Key、OIDC；写接口与读接口不同权限 |
| **观测** | Prometheus **`register_center_agents_total`** 等 |

---

## 9. 实现清单（建议 PR 拆分）

1. **`register-center/`** 包：`models.py`（Pydantic）、`store.py`（内存表）、`app.py`（FastAPI 路由）；
2. **入口脚本** + **`requirements`** 或与主仓库共用 **`requirements.txt`**（若只多 `fastapi/uvicorn` 而已有则复用）；
3. **单元测试**：POST 覆盖、GET 筛选、DELETE、URL 规范化；
4. **（可选）** DAgents **`app/config/settings.py`** 增加 **`registry_url`** / **`discovery_group`** / **`agent_public_base_url`**，并在 API lifespan 钩子中自登记。

---

## 10. 非目标（本阶段明确不做）

- 作为 **API 网关** 转发用户对话流量；
- 替代 **DNS / K8s Service** 做底层网络解析；
- 在 MVP 内保证 **跨重启** 登记不丢（无持久化即接受清空）。

---

## 11. 修订记录

| 日期 | 说明 |
|------|------|
| （初稿） | MVP API、数据字段、`discovery_group` |
| 2026-04-12 | 扩充：架构图、请求/响应示例、DELETE、错误表、实现清单、与 **`REGISTRY_URL`** 衔接 |
