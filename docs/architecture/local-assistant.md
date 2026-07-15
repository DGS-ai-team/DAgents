# 本地单 Agent 助手：多 Client 模式

本文描述 **Phase AC** 目标形态：**Go Agent Node** 为唯一运行时，人机交互通过 **终端或浏览器 Client** 连接本机 Node，用户按环境自行选择。

Manage、远程多 Agent 统一运维入口 **不在本文范围**（见 [three-component-model.md](../design/three-component-model.md)）；本机浏览器 Client 为 Node 内嵌 **`/ui/`**，非历史独立 DAgentsUI 仓库。

---

## 1. 组件职责

| 组件 | 路径 | 职责 |
|------|------|------|
| **Agent Node** | `node/` | LLM turn、工具、session、SQLite；`HTTP + SSE` API |
| **Go Client（full + repl）** | `client/` | SSH 默认全屏 TUI；`--plain` 行模式兜底 |
| **Python Textual TUI** | `app/cli/` | 现代终端首选：`dagents chat` |
| **Node Web UI** | `node/webui/` | 浏览器 Client：`http://127.0.0.1:<port>/ui/` |

```text
┌──────────────────┐     HTTP/SSE      ┌─────────────────┐
│ Python Textual   │ ────────────────► │                 │
│ dagents chat     │                   │  Agent Node     │
├──────────────────┤ ────────────────► │  (Go)           │
│ Go REPL          │                   │  127.0.0.1:port │
│ dagents-client   │                   │  + GET /ui/     │
├──────────────────┤ ────────────────► └─────────────────┘
│ 浏览器 /ui/      │
└──────────────────┘
```

---

## 2. 如何选择 Client

| 环境 | 推荐 | 命令 |
|------|------|------|
| 现代终端（WSL、新 Linux、Windows Terminal） | **Python Textual** | `dagents chat`（读共用 `config.yaml`） |
| 老 SSH、RHEL6、无 Python、脚本 | **Go Client** | `dagents-client tui`（全屏）；`tui --plain` 兜底 |
| 老 Windows + 浏览器（Chrome/Edge） | **Node Web UI** | 启动 Node 后打开 `http://127.0.0.1:<port>/ui/` |
| 探活 | 任意 | `dagents-client probe` / `curl …/health` |

**共用配置文件**

- 路径：`packaging/agent-client/config.yaml`（从 `config.example.yaml` 复制）
- 查找顺序：`--config` / `-config` → 环境变量 `DAGENTS_CONFIG` → 上述默认路径
- Node 读 `listen` / `llm` / `fs_root` 等；Client 读 `local.endpoint`

Python TUI 直连 Node（SSE 按 `session_id` 过滤，见 `app/cli/api_client.py`）。

---

## 3. 功能范围（刻意保持简单）

**共有（连 Node 时）**

- 创建 / 恢复 session、发消息、SSE 流式回复
- HITL：工具审批、`ask_user_information`（本地 **`hitl_required`** 弹 UI；`done` 仅表示轮到用户，见 [agent-node-api.md §2.4.1](./agent-node-api.md)）
- `POST /v1/sessions/{id}/cancel` 取消在途 turn
- `/clear`、`/status`、`/sessions`（各 Client 子集）
- session skills（`/skill`、HTTP API）

**Go REPL 独有定位**

- 无 Textual 依赖；老终端可读写
- 斜杠命令：`/cancel`、`/history`、`/tools brief|verbose`

**Node 提供**

- triggers 工具与 `/v1/triggers` HTTP API（见 `node/internal/triggers`）

**Node 暂不提供**

---

## 4. 本地运行（最小联调）

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
# 编辑 config.yaml（llm、agent_id 等）
```

**终端 1 — Node**

```bash
go run ./node/cmd/dagents-node
```

**终端 2a — Textual（推荐）**

```bash
dagents chat
```

**终端 2b — Go REPL（兜底）**

```bash
go run ./client/cmd/dagents-client tui
```

---

## 5. 取消在途 turn

| Client | 方式 |
|--------|------|
| Python Textual | `Esc` |
| Go REPL | `/cancel` |
| API | `POST /v1/sessions/{session_id}/cancel` |

取消后 **不退出** Client；session 与 SSE 订阅保持。

---

## 6. 与 v2 总文档的关系

- [agent-client-refactor-plan.md](../design/agent-client-refactor-plan.md) 原写「Go TUI 替代 Python textual」；**现改为双 Client 并存**，Go REPL 为兜底而非唯一 TUI。
- [three-component-model.md](../design/three-component-model.md) 中「Client 仅连本机 Node」仍成立；**Client 实现**可为 Python 或 Go。
- Manage 启用与否不影响本地助手闭环（`manage.enabled: false`）。
