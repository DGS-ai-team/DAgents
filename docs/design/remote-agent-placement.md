# 远端 Agent Placement 与观测面（设计）

> **状态**：P5 Registry `node_id` 身份落地（窄范围；2026-07-29）  
> **分支**：`cursor/remote-agent-placement-7e3e`（大动作独立演进；**不合入 dev 直至评审通过**）  
> **范围**：Node + Web UI + Manage 控制面；屏幕旁观 MVP；**不含**键鼠控制、**不含** `sandbox.backend=remote`

本文落实产品决策，并回答「是否要改造 Client–Node–Manage 通信」。结论：**要改，而且应把「业务 A2A」与「控制面 / 数据面代理」拆开**；继续在现有「本机 Node 逐 API 反代」上打补丁会不可维护。

---

## 0. 已拍板的产品决策

| # | 决策 |
|---|------|
| 1 | 远端 Agent 的主人 = **创建 Node（owner_node）** + **所在 Node（home_node）** |
| 2 | 远程屏幕先做 **只旁观**（低帧率截帧），不做键鼠 |
| 3 | 无 GUI 的 OS **允许**创建远端 Agent，**不展示**屏幕入口 |
| 4 | 删除 = **销毁 home 上实例 + 删除 owner 上引用**（双删） |
| 5 | `sandbox.backend=remote` **与本需求无关**；设置页不再提供该选项，避免与 Placement 混淆 |

---

## 1. 概念分离（强制）

| 概念 | 含义 | 状态 |
|------|------|------|
| **Placement（放置）** | Agent 实例跑在同组其他 Node 上；本地保留引用 | **本设计主线** |
| **A2A `agent_invoke`** | 请对端 Node/能力帮做**一次任务**；不拥有对端长期 Agent | 已有，继续保留 |
| **`sandbox.backend=docker`** | 本机容器隔离执行环境 | 已有 |
| **`sandbox.backend=remote`** | 外部沙箱 HTTP 运行时（预留） | **从 UI 下线**；API 暂保留读旧快照，不再引导新建 |
| **`origin=remote` stub** | 本地视图里的远端引用 | 升级为正式 Placement 引用记录 |

禁止在文案/设置里把「远程沙箱」写成「远程电脑上的 Agent」。

---

## 2. 为什么要动通信架构

### 2.1 现状问题

当前硬约束（手册）：

- Client / Web UI → **只连本机 Node**
- Node ↔ Node **禁止直连**
- 跨 Node 协作走 Manage（A2A 任务）

若在此模型上做 Placement，本机 Node 必须把远端 Agent 的几乎全部 API 反代一遍：

- `messages` / `streams` / `resume` / `hydrate` / `context` / `media` / `workspace-activity` / `settings` / `skills` / `cancel` / …

代价：

1. **漏代理即半残**（已在评审中列为最高风险）
2. SSE/大媒体经本机双跳，延迟与故障面变差
3. HITL、子 Agent、tool-jobs 状态机在代理层极易不一致
4. A2A 通道语义是「任务」，硬塞「代建 Agent / 代订屏幕流」会污染协议

### 2.2 目标通信模型（推荐）

引入三层，而不是「一切打进 A2A」：

```text
┌──────────────┐     ┌──────────────────┐     ┌──────────────────┐
│ Web UI       │────▶│ 本机 Node（入口） │────▶│ Manage           │
│              │     │ · 会话/登录锚点   │     │ · Registry       │
│              │     │ · Placement 目录  │     │ · Control Plane  │
│              │     │ · Ticket 兑换     │     │ · Audit          │
└──────┬───────┘     └────────┬─────────┘     └────────┬─────────┘
       │                      │                        │
       │   Edge ticket        │  control RPCs          │
       │   (短时、audience=   │  create/delete/        │ 签发 ticket
       │    home_node)        │  screen.subscribe      │ 持有 base_url
       ▼                      ▼                        ▼
┌──────────────────────────────────────────────────────────────┐
│ home Node：真正的 Agent Runtime + Screen Publisher           │
│ Web UI 经 Manage Edge Tunnel 或 Ticketed 直连（见下）访问     │
└──────────────────────────────────────────────────────────────┘
```

**推荐落地形态：Manage Edge Tunnel（首选）**

- Web UI **仍然只配置一个本机 Node 地址**（用户心智不变）
- 对本机 Agent：路径不变 `/v1/agents/{id}/...`
- 对远端 Agent：本机 Node 识别 stub 后，把请求升级为  
  `Manage: POST /v1/edge/sessions` → 拿到 `edge_session_id`  
  随后同连接或专用通道 ` /v1/edge/{session}/proxy/...` **由 Manage 转发到 home `base_url`**
- Web UI 无感知 home URL；**不恢复 Node↔Node 直连**；浏览器也不直连远端 Node

备选（第二阶段再评估）：

- **Ticketed browser→home**：Manage 签发短时 ticket，Web UI 临时连 home Node。实现简单、延迟更好，但破坏「浏览器只信本机」与 CORS/Cookie 模型，防火墙场景更差。

**本设计默认采用 Manage Edge Tunnel。**

### 2.3 与现有 A2A 的边界

| | Control Plane（新） | A2A Tasks（旧） |
|--|---------------------|-----------------|
| 用途 | 创建/删除/授权/屏幕订阅/目录同步 | 跨 Agent 业务协作 |
| 身份 | `owner_node` + `home_node` + `agent_id` | caller/callee agent（今日偏 node 级） |
| 可靠性 | 强一致、可审计 | 任务终态即可 |
| HITL | Placement Agent 的 HITL 走 **Edge 上的同一 SSE** | A2A requires_input 中继 |

并行推进（可同分支后期做）：Registry 身份从「几乎等于 node_id」迁到 **`node_id + agent_id`**（见 `agent-instance-model.md` Phase 5），否则远端实例无法被组内正确寻址。

---

## 3. 数据模型

### 3.1 home Node 上的真实 Agent

与今日本地 Agent 相同（`store.Agent` + `config_snapshot`），额外持久化：

```json
{
  "agent_id": "agt_...",
  "placement": {
    "role": "home",
    "owner_node_id": "node_create_01",
    "home_node_id": "node_home_01"
  }
}
```

主人语义：

- **owner_node**：有权删除、改展示名（策略可再收紧）、发起屏幕订阅
- **home_node**：实际跑 runtime、持有 workspace/LLM 绑定、可拒绝创建/屏幕（本地策略）

### 3.2 owner Node 上的引用（stub）

```json
{
  "agent_id": "agt_...",
  "origin": "remote",
  "display_name": "...",
  "placement": {
    "role": "owner_ref",
    "owner_node_id": "node_create_01",
    "home_node_id": "node_home_01",
    "home_agent_id": "agt_...",
    "status": "online|offline|degraded"
  },
  "host": {
    "os_kind": "windows|linux|darwin|unknown",
    "sys_platform": "windows",
    "machine": "amd64",
    "display_available": false,
    "display_label": "Windows 11"
  }
}
```

约定：

- **同一 `agent_id`** 在 owner 引用与 home 实例间保持一致（由 home 创建时生成，回传 owner），避免两侧各造 UUID。
- 列表 API：本机 `GET /v1/agents` = 本地实例 ∪ owner 引用（可标 `origin`）。
- `config_snapshot` 对 stub **可不存全量**；设置页打开时经 Edge 拉 home 真源。

### 3.3 主机能力（心跳）

扩展 Node→Manage 注册/心跳 `metadata`：

```json
{
  "host_info": { "...现有..." },
  "display": {
    "available": true,
    "backend": "dxgi|sck|x11|none",
    "reason_if_unavailable": "no_display"
  },
  "placement": {
    "allow_peer_create": true,
    "allow_screen_view": true
  }
}
```

`display.available=false` ⇒ 允许 Placement，UI 隐藏屏幕块。

---

## 4. API 草案（Control + Edge）

> 路径名为草案，实现时可微调；原则是 **控制面在 Manage，数据面经 Edge**。

### 4.1 本机 Node（Web UI 友好）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/peers/nodes` | 同组可放置 Node 列表（代理 Manage + 过滤） |
| POST | `/v1/agents` | body 增 `placement.home_node_id`；本机则不变 |
| DELETE | `/v1/agents/{id}` | 若 `origin=remote`：Control 双删 |
| GET | `/v1/agents` | 合并本地 + 引用 |
| \* | `/v1/agents/{id}/**` | stub 则 **upgrade 到 Edge proxy**（统一入口，禁止逐路由手写） |

屏幕（旁观）：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/agents/{id}/screen/stream` | SSE/WS：JPEG/WebP 帧；无 GUI → `404 screen_unavailable` |

### 4.2 Manage Control Plane

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/control/nodes/{home}/agents` | owner 请求在 home 创建；Manage 校验同组与 `allow_peer_create` |
| DELETE | `/v1/control/nodes/{home}/agents/{id}` | 双删协调：先 home 再通知 owner（或事务表） |
| POST | `/v1/edge/sessions` | `{home_node_id, agent_id, scopes:[...]}` → `edge_session_id` |
| ANY | `/v1/edge/{session}/…` | 反代到 home；scopes 限制路径前缀 |

鉴权：

- Node 调 Manage：现有 node token / agent header 升级为 **node 凭证 + owner 声明**
- 审计：谁在何时对哪台 home 创建/删除/订阅屏幕

### 4.3 home Node 内部

新增（仅接受来自 Manage Edge 的调用，校验 `X-DAgents-Edge-Audience` 等）：

- 常规 `/v1/agents` 创建（带 `placement.owner_node_id`）
- `GET /v1/screen/publish` 或内部 publisher 挂到 agent 维度流

---

## 5. 创建 / 删除流程

### 5.1 创建

```text
WebUI → owner Node POST /v1/agents { placement.home_node_id, ... }
  → Manage POST /v1/control/nodes/{home}/agents
    → 校验 discovery_group 交集、home online、allow_peer_create、协议版本
    → home Node POST /v1/agents（生成 agent_id，写入 placement.home）
    → 返回 agent_id + host 快照
  → owner Node 写入 origin=remote 引用
  → 返回统一 agentView
```

失败：home 拒绝 / 超时 → 不写本地引用；返回明确 `peer_create_failed`。

### 5.2 删除（决策 4）

```text
WebUI → owner DELETE /v1/agents/{id}
  → Manage control DELETE
    → home 停 turn、释放 sandbox、删库与工作区（按现有本地删除语义）
    → owner 删引用
  → 若 home 已离线：标记 pending_tombstone，home 上线补偿删除；owner 引用可先删或标 deleting
```

在 home 本机 UI 删除「别人的」Agent：需校验调用者是 owner 或 home 管理员策略（第一版：**仅 owner 或 home 本机用户**可删）。

---

## 6. 屏幕旁观（Phase 2，本分支后期）

- Publisher 在 **home Node**，按 `display.available` 启停
- 默认小窗：≤480p、≤2fps、WebP/JPEG
- Activity 右栏区块：仅 `origin=remote && display_available`
- 放大：只读 overlay；**无键鼠**
- 经 Edge scope=`screen:view`；帧默认不落盘
- 与 Agent 工具无强互斥（旁观不抢控制权）；后续可控模式再定义冲突策略

平台备注：

- Windows：DXGI（可降级 GDI）
- macOS：ScreenCaptureKit + 录屏权限
- Linux：有 `$DISPLAY`/PipeWire 才 `available=true`，否则静默无入口

---

## 7. Web UI 改动要点

1. **创建向导**：增加「运行位置：本机 / 同组 Node」；选 Node 时展示 OS 徽章与 `display_available`
2. **Agent 列表**：远端徽章 + OS；离线灰显
3. **设置 › Agent**：沙箱模式 **仅 Docker**（本提交起）；Placement 与沙箱分区展示
4. **Activity**：预留「远程桌面（旁观）」section（Phase 2 接通）
5. **命名**：现有 `remoteWorkers.js`（临时子 Agent）建议后续改名 `tempWorkers`，避免与 Placement 混淆

---

## 8. 分期（本分支内）

| Phase | 内容 | 破坏面 |
|-------|------|--------|
| **P0** | 设计冻结文档；UI 去掉远程沙箱误导；术语分离 | 小 |
| **P1** | Manage Control：peers 列表 + 远端创建/双删；owner 引用；列表 OS | 中（Manage+Node API） |
| **P2** | Manage Edge Tunnel：远端 Agent 的 `/v1/agents/{id}/**` + messages/streams 统一代理 | **大**（通信面）✅ |
| **P3** | 聊天/SSE/HITL 走 Edge 闭环（agent 绑定、SSE 冲刷、禁本地 runtime） | 大 ✅ |
| **P4** | Screen 旁观 SSE（stub 帧 + Activity 入口；无键鼠） | 中 ✅ |
| **P5** | Registry `node_id` 一等字段（双写兼容；A2A 实例寻址另开） | 大 ✅ 窄范围 |

P5 窄范围：Manage 注册/列表/discover 暴露 `node_id`；接受仅 `node_id` 或仅 `agent_id`；主键仍为 node 级；`can_a2a_invoke` 仍按 node 组交集。实例级 A2A inbox 不在本切片。

---

## 9. 明确非目标（本设计）

- 键鼠远程控制、剪贴板、文件拖拽
- 浏览器直连 home Node（第一版）
- 用 A2A task 冒充 Placement
- 把 Placement 塞进 `sandbox.backend=remote`
- 跨 Node 的 child-agent 放置

---

## 10. 开放实现细节（实现时再定，不阻塞 P0/P1）

- Edge 用 HTTP/2 多路还是独立 WS
- pending_tombstone 的存储（Manage 表 vs owner 本地）
- owner 与 home「同时在线改同一 Agent 设置」的冲突（建议 home 为真源 + 乐观锁 `updated_at`）
- 组内其他人是否只读可见该远端 Agent（当前决策只定义双主人；**默认仅双主人可见**，组内共享另开需求）

---

## 11. 相关代码锚点（现状）

- Agent `origin` 预留：`node/internal/store/agents.go`、`agents_api.go`
- 沙箱 remote 预留：`node/internal/api/agent_settings.go`、`AgentSettingsForm.vue`
- Manage 注册 host_info：`node/internal/manage/registrar.go`、`node/internal/hostsnapshot`
- 同组发现：`manage/registry/store.py` `discover`
- 右栏：`ActivityPanel.vue` / `chromeStore.panel`
- 实例模型总册：`docs/design/agent-instance-model.md`
