# 03 · API 与 Client

## 本章回答什么问题

读完本章，你应能：

- 查阅 Agent Node 对外 HTTP/SSE 契约并对照 `node/internal/api/` 实现  
- 构造 HITL `resume` 请求  
- 选择 Python Textual 或 Go Client，并理解二者差异  
- 配置 `packaging/agent-client/config.yaml` 并联调  

---

## 1. API 设计原则

| 原则 | 说明 |
|------|------|
| **用户面 = Agent** | 1 Agent = 1 主对话；优先 `/v1/agents/{agent_id}/...` |
| **`/v1/sessions*` 已下线** | 固定 410（`sessions_moved`）；一律用 `/v1/agents/{agent_id}/...` |
| **Policy / 侧车按 Agent** | SQLite（`agents.db`）；全局 `/v1/policy` 已 410 |
| **Client 只连 Node** | 默认同机 `127.0.0.1`；**前后端分离**时 Client 在较新机器、`local.endpoint` 指向目标机 Node（见 [01 §1.5](./01-愿景与架构.md)、§4.4） |
| **思考与工具在 Node 内** | 无 Backend 代执行 |
| **A2A 经 Manage** | 注册载荷用 `node_id`（`manage.enabled` 默认关） |

### 1.1 路径前缀

| 前缀 | 调用方 |
|------|--------|
| `/health` | 探活 |
| `/v1/agents/...` | **主契约**（对话、策略、侧车、子 Agent） |
| `/v1/...` | messages、streams、triggers、setup |
| `/v1/sessions/...` | 已下线（410） |

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

常见 `code`：`invalid_agent`、`agent_not_found`、`turn_busy`、`policy_denied`、`approval_required`、`llm_error`、`tool_error`、`policy_moved`、`sessions_moved`。

---

## 2. 核心端点

### 2.1 健康与元数据

```http
GET /health
→ { "status": "ok", "agent_id": "...", "version": "0.5.1" }
```

`version` 与 `node/internal/version/version.go` 及当前发版 tag 一致；**全项目唯一语义化版本**，Client/TUI 启动时探活读取，不维护独立 Client 版本号。

```http
GET /v1/agent/info
→ { "agent_id", "expose_to_peers", "capabilities", "manage_registered", "llm": { ... } }
```

### 2.2 Agents（主契约）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/agents` | 创建 Agent |
| GET | `/v1/agents` | 列表 |
| DELETE | `/v1/agents/{id}` | 归档 |
| POST | `/v1/agents/{id}/ensure` | 装入运行时 |
| GET | `/v1/agents/{id}/hydrate` | transcript + pending HITL |
| POST | `/v1/agents/{id}/cancel` | 取消在途 turn |
| POST | `/v1/agents/{id}/clear-context` | 清空上下文 |
| GET | `/v1/agents/{id}/context` | token 估算 + system prompt 预览 |
| GET | `/v1/agents/{id}/child-agents` | 临时子 Agent 列表 |

`/v1/sessions*` 已下线（410）。

### 2.3 消息与 resume

```http
POST /v1/messages
Content-Type: application/json

{
  "session_id": "sess-...",
  "request_type": "message",
  "content": "列出当前目录"
}
```

`request_type`：`message` | `resume`。

**审批 resume**（字段以 `node/internal/hitl/resume.go` 为准）：

```json
{
  "session_id": "sess-...",
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
GET /v1/stream?after_seq=0
```

- 单 TUI 通常 **一个 SSE 连接**；事件带 `session_id` 字段供过滤。  
- `after_seq`：断点续传；见 [02 §4.6](./02-Agent-Node-核心.md)。  
- 事件类型速查：[附录/SSE事件速查](./附录/SSE事件速查.md)。

**关键事件**：

| 事件 | 含义 |
|------|------|
| `assistant_delta` | 流式正文 |
| `reasoning_delta` | 推理链（若模型支持） |
| `tool_call` / `tool_result` | 工具调用与结果 |
| `hitl_required` | 本地 turn 统一 HITL（`items[]` 含 ask / 审批） |
| `approval_required` | 需人工审批（A2A / 子 Agent） |
| `user_information_required` | 需用户输入（A2A 中继） |
| `usage` | token 统计（**独占一行**展示） |
| `done` | 本步 turn 结束（**不等于**整个多步工具链结束） |

---

## 3. Client 架构

```text
┌──────────────────┐     HTTP/SSE      ┌─────────────────┐
│ Python Textual   │ ────────────────► │                 │
│ dagents chat     │                   │  Agent Node     │
├──────────────────┤ ────────────────► │  127.0.0.1:port │
│ Go bubbletea     │                   │  + /ui/ Web UI  │
│ dagents-client   │                   └─────────────────┘
├──────────────────┤ ────────────────►
│ 浏览器 /ui/      │
└──────────────────┘
```

| 环境 | 推荐 | 命令 |
|------|------|------|
| WSL、新 Linux、Windows Terminal | Python Textual | `dagents chat` |
| SSH 全屏、无 Python | Go full TUI | `dagents-client tui` |
| RHEL6、`TERM=dumb` | Go plain REPL | `dagents-client tui --plain` |
| 老 Windows + 浏览器 | Node Web UI | `http://127.0.0.1:<port>/ui/`（`ui.enabled: true`） |
| 探活 | 任意 | `dagents-client probe` / `curl /health` |

### 3.1 源码索引

| Client | 路径 | 要点 |
|--------|------|------|
| Python TUI | `app/cli/tui/app.py` | Textual 组件、HITL 弹窗 |
| Python API | `app/cli/api_client.py` | SSE 按 session 过滤 |
| Python HITL | `app/cli/hitl_batch.py` | `expand_hitl_required` |
| Python A2A relay | `app/cli/child_agent.py` | `a2a_relay` 工具块样式 |
| Go full TUI | `client/internal/tui/full/` | bubbletea、HITL 队列 |
| Go HITL | `client/internal/hitl/` | `hitl_batch.go` 展开、`approval`、A2A relay |
| Go A2A 展示 | `client/internal/tui/full/a2a_relay_tools.go` | `from <对端>` 标识 |
| Go plain REPL | `client/internal/tui/repl/` | 行模式 |
| Node Web UI | `node/webui/frontend/` | Vue 3 + Vite；构建产物 **不入库**；`go:embed` 挂载 `/ui/` |
| Web UI API | `node/webui/frontend/src/api/node.js` | 复用 `/v1` HTTP/SSE |

### 3.2 HITL 与 Client 行为

- Client 维护 **非阻塞 HITL 队列**；收到 **`hitl_required`** 后按 `hitl_type` 展开入队（先 ask_user，再 approval）；用户分步 `POST resume`（`user_information` / `approval`）。仍兼容 `approval_required` / `user_information_required`（A2A）。  
- **Esc**：取消在途 turn（`POST .../cancel`），非 `/cancel` 斜杠。  
- **A2A relay**（v0.3.9）：caller 侧收到 relay 审批后，提交 Manage `caller_resume`；TUI **不等**本地 `tool_result`，审批后直接终态（青点 → 灰点）。  
- 源码：`client/internal/hitl/a2a.go`、`node/internal/session/a2a_caller_hitl.go`。

### 3.3 转录展示约定

- **usage** 独占一行（右对齐），不与 assistant 末行拼接。  
- A2A 工具行带 **`from <callee_agent_id>`** 后缀与专用样式。  
- Go：`client/internal/tui/shared/transcript*.go`；Python：`app/cli/tui/app.py`。

---

## 4. 配置与同包发布

### 4.1 配置文件

**路径**：`packaging/agent-client/config.yaml`（从 `config.example.yaml` 复制）

**查找顺序**：CLI `-config` → 环境变量 `DAGENTS_CONFIG` → 默认 `packaging/agent-client/config.yaml`

| 读者 | 常用键 |
|------|--------|
| Node | `listen`、`llm`、`fs_root`、`manage`、`expose_to_peers` |
| Client | `local.endpoint` |

完整键表：[附录/配置项参考](./附录/配置项参考.md)。

### 4.4 前后端分离（跨机 Client）

老旧目标机上跑 **Node**，较新机器上跑 **Client** 时，需拆成两份配置（或同文件在不同机器各取所需段）：

**目标机（后端）** — 仅运行 `dagents-node`：

```yaml
listen:
  host: 0.0.0.0          # 或内网 IP；勿在未加固环境裸奔公网
  port: 18765
local:
  endpoint: http://10.0.1.50:18765   # 与 listen 一致，供 Manage registration.base_url 等
fs_root: ./.runtime
llm: { ... }
```

**较新机器（前端）** — 仅运行 `dagents chat` 或 `dagents-client tui`：

```yaml
local:
  endpoint: http://10.0.1.50:18765    # 指向目标机 Node
# 无需 listen / llm / fs_root（Client 不执行工具）
```

工具、SQLite、policy 仍在**目标机** `fs_root` 上；前端只负责 HTTP/SSE 与 HITL UI。内网部署建议配合防火墙白名单；更严场景可用 SSH 本地转发（Client 仍连 `127.0.0.1`，隧道指向目标机）。

设计背景：[01-愿景与架构 §1.5 策略 B](./01-愿景与架构.md)。

### 4.5 工作区布局（相对 `fs_root`）

| 子目录 | 用途 |
|--------|------|
| `memory/sessions.db` | SQLite |
| `policy/` | 审批策略 |
| `skills/` | skills 目录 |
| `history/` | 原始 message journal（可选） |
| `triggers/` | trigger 持久化 |
| `data/` | Agent 临时工作区 |
| `prompt_context/` | soul / user / long_term |

### 4.6 发布形态

- Release：`dagents-local-assistant-*`（Linux tarball / Windows zip / 安装包）  
- `dagents chat|tui --withnode`：自动后台起 Node  
- 详见 [06 §2 打包与安装](./06-运维与案例.md#2-打包与安装)

---

## 5. 源码与配置索引

| 概念 | 路径 |
|------|------|
| 路由注册 | `node/internal/api/server.go` |
| 入队 messages | `node/internal/api/messages.go` |
| resume 解析 | `node/internal/hitl/resume.go` |
| SSE handler | `node/internal/api/stream.go` |
| 配置加载 | `shared/config/config.go` |
| Python 入口 | `run_dagents_cli.py` |
| Go Client 入口 | `client/cmd/dagents-client/` |

模块 REFERENCE：`node/internal/api/REFERENCE.md`（若有）、`shared/config/REFERENCE.md`

---

## 6. 快速上手

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml

# 首次 go test / go build 前（Web UI 静态资源不入库）
bash node/webui/build.sh

# 终端 1
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml

# 终端 2（任选）
dagents chat
# 或
go run ./client/cmd/dagents-client tui -config packaging/agent-client/config.yaml
```

`dagents version` / `dagents-client version` / TUI 欢迎区均展示 **`GET /health` 的 `version`**（需 Node 在线）。

默认 `llm.mock: true` 时可无 API Key 联调。

---

## 7. 下一章

→ [04-能力与策略](./04-能力与策略.md)：内置工具、policy、skills、triggers、子 Agent、压缩。
