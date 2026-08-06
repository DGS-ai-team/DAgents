# 03 · API 与 Client

## 本章回答什么问题

读完本章，你应能：

- 查阅 Agent Node 对外 HTTP/SSE 契约并对照 `node/internal/api/` 实现  
- 构造 HITL `resume` 请求  
- 使用 **Web UI**（默认）并联调 Node  
- 配置 `packaging/agent-client/config.yaml`  

> **v0.9.1**：人机默认入口是 **内嵌 Web UI**（`/ui/`）。`dagents-client` 仅保留 probe / update / version；对话型 TUI/CLI 已移除。下文若出现历史 TUI 命令，仅作归档说明。

---

## 1. API 设计原则

| 原则 | 说明 |
|------|------|
| **用户面 = Agent** | 1 Agent = 1 主对话；主路径 `/v1/agents/{agent_id}/...` |
| **`/v1/sessions*` 已移除** | 未注册路由（404） |
| **Policy / 侧车按 Agent** | SQLite（`agents.db`）；全局 `/v1/policy` 已移除（404） |
| **UI / 工具只连 Node** | 浏览器打开本机 `/ui/`；工具在 Node 内执行 |
| **跨 Node = Workgroup** | Placement / A2A inbox 已拆除；注册用 `node_id` |

### 1.1 路径前缀

| 前缀 | 调用方 |
|------|--------|
| `/health` | 探活 |
| `/ui/` | 内嵌 Web UI 静态资源 |
| `/v1/agents/...` | **主契约**（对话、策略、侧车、子 Agent） |
| `/v1/workgroups/...` | 工作组（Node 反代 Manage；需启用 manage.workgroup） |
| `/v1/...` | messages、streams、triggers、setup |

权威契约：[agent-node-api.md](../architecture/agent-node-api.md) · OpenAPI：[openapi-node.yaml](../architecture/openapi-node.yaml)

### 1.2 通用错误体

```json
{
  "error": {
    "code": "agent_not_found",
    "message": "agent 不存在",
    "details": { "agent_id": "agt-xxx" }
  }
}
```

常见 `code`：`invalid_agent`、`agent_not_found`、`turn_busy`、`policy_denied`、`approval_required`、`llm_error`、`tool_error`。

---

## 2. 核心端点

### 2.1 健康与元数据

```http
GET /health
→ { "status": "ok", "node_id": "...", "version": "0.9.1" }
```

`version` 与 `node/internal/version/version.go` 及发版 tag 一致（全项目唯一语义化版本）。

```http
GET /v1/agent/info
→ { "node_id", "capabilities", "manage_registered", "llm": { ... } }
```

### 2.2 Agents（主契约）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/agents` | 创建 Agent（勿再传 `sandbox`） |
| GET | `/v1/agents` | 列表 |
| DELETE | `/v1/agents/{id}` | 归档 |
| POST | `/v1/agents/{id}/ensure` | 装入运行时 |
| GET | `/v1/agents/{id}/hydrate` | transcript + pending HITL |
| POST | `/v1/agents/{id}/cancel` | 取消在途 turn |
| POST | `/v1/agents/{id}/clear-context` | 清空上下文 |
| GET | `/v1/agents/{id}/context` | token 估算 + system prompt 预览 |
| GET | `/v1/agents/{id}/child-agents` | 临时子 Agent 列表 |

### 2.3 消息与 resume

```http
POST /v1/messages
Content-Type: application/json

{
  "agent_id": "agt-...",
  "request_type": "message",
  "content": "列出当前目录"
}
```

`request_type`：`message` | `resume`。

**审批 resume**（字段以 `node/internal/hitl/resume.go` 为准）：

```json
{
  "agent_id": "agt-...",
  "request_type": "resume",
  "resume": {
    "kind": "approval",
    "decision": "approve",
    "tool_call_id": "call_..."
  }
}
```

`decision`：`approve` | `reject` | `always`（仅 bash 等支持 always 的策略）。

### 2.4 SSE 订阅

```http
GET /v1/streams?agent_id=agt-...
```

- Web UI 通常维持一条 SSE；可用 `agent_id` query 过滤。  
- `Last-Event-ID` / `live=1`：断点与增量；见 [02 §4.6](./02-Agent-Node-核心.md)。  
- 事件类型速查：[附录/SSE事件速查](./附录/SSE事件速查.md)。

**关键事件**：

| 事件 | 含义 |
|------|------|
| `assistant_delta` | 流式正文 |
| `reasoning_delta` | 推理链（若模型支持） |
| `tool_call` / `tool_result` | 工具调用与结果 |
| `hitl_required` | 本地 turn 统一 HITL（`items[]` 含 ask / 审批） |
| `usage` | token 统计 |
| `done` | 本步 turn 结束（**不等于**整个多步工具链结束） |

---

## 3. 人机入口

### 3.1 现行：Web UI

```text
浏览器 ──HTTP/SSE──► Agent Node（:18765）
                      ├── /ui/     Vue Workbench
                      └── /v1/*    Agents · streams · workgroups 反代
```

| 场景 | 入口 |
|------|------|
| 本机对话 / 设置 / 工作组 | `http://127.0.0.1:18765/ui/` |
| 探活 | `curl /health` 或 `dagents-client probe` |
| Manage 运维 Console | `http://127.0.0.1:8020/console/`（另起 Manage） |

源码：`node/webui/frontend/`（构建产物 **不入库**；`go:embed`）。API 封装：`src/api/node.js`。

### 3.2 HITL 行为（Web UI）

- 收到 **`hitl_required`** 后按 `items[]` 入队（ask / 审批）；用户分步 `POST resume`。  
- 取消在途 turn：`POST /v1/agents/{id}/cancel`（或工作组 turn cancel）。  
- 工作组信息型 HITL：见 [07-Workgroup协作](./07-Workgroup协作.md)。

### 3.3 辅助二进制

`dagents-client`：**probe / update / version** 等运维命令；**不再**提供 `tui` / `chat` 对话模式。

---

## 4. 配置与同包发布

### 4.1 配置文件

**路径**：`packaging/agent-client/config.yaml`（从 `config.example.yaml` 复制）

**查找顺序**：CLI `-config` → 环境变量 `DAGENTS_CONFIG` → 默认路径

| 读者 | 常用键 |
|------|--------|
| Node | `listen`、`manage`、`ui`（其余多在 SQLite / Web UI） |

完整键表：[附录/配置项参考](./附录/配置项参考.md)。  
运行时根固定 `./.runtime`（不可配置）。

### 4.2 跨机访问 Node（可选）

目标机跑 Node 且需从他机浏览器打开 `/ui/` 时：

```yaml
listen:
  host: 0.0.0.0   # 或内网 IP；勿在未加固环境裸奔公网
  port: 18765
```

工具与 SQLite 仍在目标机 `fs_root`。建议防火墙白名单或 SSH 隧道。

### 4.3 工作区布局（相对 `fs_root`）

| 子目录 | 用途 |
|--------|------|
| `memory/` / `agents.db` | Agent 与消息持久化 |
| `policy/` | 审批策略（可按 Agent） |
| `skills/` | skills 目录 |
| `triggers/` | trigger 持久化 |
| `prompt_context/` | soul / user / long_term |
| `node/` | `node_id` 等 |

### 4.4 发布形态

- Release：`dagents-local-assistant-*`（Linux / Windows / 安装包）  
- Desktop Shell（可选）：托盘启停 Node  
- 详见 [06-运维与案例](./06-运维与案例.md)

---

## 5. 源码索引

| 概念 | 路径 |
|------|------|
| 路由注册 | `node/internal/api/server.go` |
| 入队 messages | `node/internal/api/messages.go` |
| resume 解析 | `node/internal/hitl/resume.go` |
| SSE handler | `node/internal/api/stream.go` |
| 工作组反代 | `node/internal/api/workgroup_api.go` |
| 配置加载 | `shared/config/config.go` |
| Web UI | `node/webui/frontend/` |
| 精简 client | `client/cmd/dagents-client/` |

---

## 6. 快速上手

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml

# Web UI 静态资源不入库（Windows 可用 npm）
npm run build --prefix node/webui/frontend
# 或：bash node/webui/build.sh

go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
# 浏览器打开 http://127.0.0.1:18765/ui/
```

默认可开 **mock LLM**（Web UI / `node_settings`）无 API Key 联调。

---

## 7. 下一章

→ [04-能力与策略](./04-能力与策略.md)：内置工具、policy、skills、triggers、子 Agent、压缩。  
→ [07-Workgroup协作](./07-Workgroup协作.md)：跨 Node 工作组。
