# Agent Node + Client 重构计划（第一步）

本文是 **implementation 主计划**：在 **Manage 之前** 完成 **Agent Node（Go）+ Client（Go TUI）** 闭环。  
背景与三组件边界见 [background-and-motivation.md](./background-and-motivation.md)、[three-component-model.md](./three-component-model.md)。

**状态（2026-06）**：`node/`、`client/`、`shared/config/` 已落地；**N0–N6 完成**；**N7 基本完成**（Release CI、Windows 安装包 **v0.2.3**、Linux **`install.sh`**、静态构建与 SysV init 已就绪；**RHEL6 真机验收**仍 open）。Python Textual 与 Go Client（**full 默认 + repl 兜底**）并存，均连 Go Node。

---

## 1. 目标与验收

### 1.1 目标

| 项 | 说明 |
|----|------|
| **Agent Node** | 单进程：LLM turn loop、工具执行、session 队列、SQLite 持久化；bind `127.0.0.1:port` |
| **Client** | Go Client（`full` bubbletea + `repl` 兜底）：只连本地 Node |
| **同包发布** | 一份配置（`agent_id`、`listen`、`local.endpoint`）；适配老旧终端 |
| **Python 现状** | 开发期保留 `app/` 作行为参考；**不**在本阶段拆 Manage |

### 1.2 阶段验收（Definition of Done）

1. `dagents-node` 启动后 `GET /health` 返回 `agent_id`。
2. `dagents-client`（或 `dagents chat` 新实现）创建 session、发消息、收 **SSE** 流式回复。
3. 至少一条工具链路：`bash_run` 或 `read_file` 在 Node 内执行并回传 `tool_result`。
4. **HITL**：`hitl_required` → Client 分步 resume → turn 继续（可与 ask_user 同批）。
5. Node 重启后 **session 可恢复**（SQLite 中有历史 messages）。
6. 在 **WSL / Linux** 上端到端演示；Windows 构建列为 N7 交叉编译目标。

---

## 2. 仓库布局（新建）

```text
node/                          # Agent Node（Go module: dagents/node）
  cmd/dagents-node/main.go
  internal/
    config/                    # YAML + env
    api/                       # HTTP + SSE（对齐 agent-node-api-sketch §2）
    session/                   # 队列、session 生命周期
    turn/                      # turn loop（移植自主 Agent）
    llm/                       # OpenAI 兼容客户端
    tools/                     # 工具 registry + executor
    store/                     # SQLite session/messages
    policy/                    # 本地 policy.yaml（Phase AC 不上 Manage）
  go.mod

client/                        # Client TUI（Go module: dagents/client）
  cmd/dagents-client/main.go
  internal/
    config/                    # 读与 node 共享的 local.endpoint
    api/                       # Node HTTP/SSE 客户端
    tui/                       # 终端 UI（bubbletea 或等价）
    hitl/                      # 审批、user_information 面板
  go.mod

packaging/
  agent-client/                # 同包示例 config、systemd unit（后续）
    config.example.yaml
```

**不恢复** 旧路径 `proxy/`、`app/harness/execution/v2/`。

---

## 3. 与 Python 现网的对应关系（移植清单）

| Go 模块 | 参考 Python（行为对齐，非复制架构） |
|---------|-------------------------------------|
| `node/internal/api` | `app/harness/api/app.py`（sessions/messages/streams 子集） |
| `node/internal/session` | `app/harness/service/agent_service.py`（队列、单 session 消费） |
| `node/internal/turn` | `app/core/main_agent/agent.py`（`run_turn`、tool 循环） |
| `node/internal/tools/*` | `app/harness/tools/{bash,fs,tool}.py` |
| `node/internal/store` | `app/harness/memory/` + sqlite session store |
| `client/internal/tui` | `app/cli/tui/app.py`（交互流程，非 textual API） |
| `client/internal/api` | `app/cli/api_client.py` |

**刻意不移植（本阶段）**：

- `connection_id` / 多 Backend SSE 分桶
- Body/Proxy/control channel / Execution Dispatcher v2
- Register Center relay、`agent_peer` 跨 Agent（留 Manage 阶段）

---

## 4. 分步实施（N0–N7）

### N0：骨架与配置（约 3–5 天）

- [x] 初始化 `node/go.mod`、`client/go.mod`、`shared/config`
- [x] 统一配置 schema（见 [agent-node-api.md](../architecture/agent-node-api.md) §7）
- [x] `dagents-node`：`GET /health`、`GET /v1/agent/info`
- [x] `dagents-client`：读配置、探测 Node 是否在线
- [x] CI：`go test ./node/...`（`.github/workflows/go-ac.yml`）

**产出**：空壳可编译、可联调配置。

---

### N1：Session + Message + SSE 壳（约 5–7 天）

- [x] `POST /v1/sessions`、`POST /v1/messages`（accept 入队即可）
- [x] `GET /v1/streams`：SSE 连接、`seq` 递增、心跳 comment
- [x] 内存 session 表；LLM turn 推送 `assistant` + `done` 事件
- [x] Client：创建 session、订阅 SSE、打印流式文本

**对齐文档**：[agent-node-api.md](../architecture/agent-node-api.md) §2.2–2.4。

**产出**：无 LLM 的「回声 + 假 done」端到端。

---

### N2：Session 队列与单轮 Turn（约 7–10 天）

- [x] per-session 队列（human / resume / tool_result 优先级）
- [x] `cancel_current_turn` → `POST /v1/sessions/{id}/cancel`
- [x] Turn 状态机与真实 LLM（OpenAI 兼容 API；配置 `llm.*`）
- [x] SSE：`assistant` 增量、`usage`、`done`、压缩事件

**产出**：纯对话、无工具的多轮 chat。

---

### N3：工具执行（约 7–10 天）

- [x] Tool registry：OpenAI tools 列表
- [x] 工具：`read_file`、`write_file`、`bash_run`、`search_*`、skills、triggers 等
- [x] Turn 内 tool_call → 本地 execute → `tool_result` → 继续 loop
- [x] SSE：`tool_call`、`tool_result`；并行工具与 `async_tool_result`

**产出**：「列出目录」「读文件」类任务可用。

---

### N4：HITL 与策略（约 5–7 天）

- [x] 本地 `.runtime/policy/*.approval.txt` 策略
- [x] HITL SSE + `POST /v1/messages` `request_type=resume`（本地 **`hitl_required`**；A2A 仍 `approval_required` / `user_information_required`）
- [x] `ask_user_information` + 审批工具可**同批** pending，Client 分步 resume
- [x] Client：Textual / Go full+repl / Web UI 审批与询问 UI

**产出**：bash 审批链路与 v1 体验等价。

---

### N5：持久化（约 4–6 天）

- [x] SQLite：`sessions`、`messages`（OpenAI 消息 JSON）
- [x] 启动加载 session；`clear-context`、`release session`
- [x] 原始消息 JSONL 审计（`node/internal/history`）

**产出**：重启 Node 后续聊。

---

### N6：Client TUI 完整化（约 7–10 天）

- [x] 多 session、`/status`、历史、tool 输出
- [x] SSE 断线重连（`Last-Event-ID`）
- [x] Python Textual TUI（`dagents chat`）与 Go Client（full/repl）并存
- [x] Go bubbletea 全屏 TUI（`client/internal/tui/full/`）+ `--plain` REPL 回退
- [x] 文档：[client-packaging.md](../architecture/client-packaging.md)

**产出**：日常本地助手可用；N7 发布物待完成。

---

### N7：老旧 OS 与发布（约 5–7 天）

- [x] 编写 [go-node-compatibility.md](../architecture/go-node-compatibility.md)（glibc / Win2012 构建矩阵）
- [x] 静态链接构建脚本（`scripts/ci/build_go_linux_static.sh`、`scripts/package_go_agent_client.sh`）
- [x] RHEL 6 SysV init（`scripts/linux/install_node_service_sysv.sh`）
- [x] Windows **交叉编译** smoke（`go-ac.yml`）；发布 tarball/zip 接入 GitHub Releases（linux + **windows-amd64**）
- [x] Windows **Inno Setup 安装包**（`v0.2.3`）；Linux **`dagents` + `install.sh`**
- [ ] RHEL 6.9 / Win2012 **真机验收**记录（见 [rhel6-acceptance-checklist.md](../architecture/rhel6-acceptance-checklist.md)）
- [x] 单测 + 集成测试（`go test ./node/...`）

**产出**：可在目标老旧 Linux 上解压即用。

---

## 5. Manage 在本阶段的处理方式

| 能力 | Phase AC 行为 |
|------|----------------|
| 注册 / 心跳 | **关闭**（`manage.enabled: false`）或 no-op 日志 |
| 审计上报 | 写本地 JSONL（`audit.log`），Manage 阶段再 upload |
| A2A | 工具未注册或返回「Manage 未启用」 |
| `manage_registered` | `/v1/agent/info` 返回 `false` |

避免 Node 依赖 Manage 才能启动。

---

## 6. API 与事件冻结范围（M0 子集）

**HTTP（必须）**

- `GET /health`
- `GET /v1/agent/info`
- `POST /v1/sessions`、`GET /v1/sessions`
- `POST /v1/messages`
- `GET /v1/streams`
- `POST /v1/sessions/{id}/cancel`
- `POST /v1/sessions/{id}/clear-context`

**SSE 事件（必须）**

- `assistant`、`tool_call`、`tool_result`
- `hitl_required`（本地 turn）；A2A 中继仍可能为 `approval_required` / `user_information_required`
- `error`、`done`

**HTTP（N5+ 可选）**

- `GET /v1/sessions/{id}/context`
- skills HTTP

完整列表见 [agent-node-api.md](../architecture/agent-node-api.md)。

---

## 7. 风险与约束

| 风险 | 缓解 |
|------|------|
| Turn loop 移植遗漏边界 | 对照 `tests/test_main_agent_orchestrator.py` 逐条勾选 |
| 老 Windows TUI | Client 用 Go + 简单 ANSI；避免 OSC/复杂鼠标 |
| LLM 协议差异 | `llm/` 抽象；先 OpenAI 一家 |
| 与 Python 双栈维护 | Python Agent 路径已标记 **deprecated**（见 `app/deprecated_backend.py`）；Go Node 为本地助手主线 |

---

## 8. 完成后的下一步（不在本计划内）

1. **Manage MVP** — [manage-api-sketch.md](../future/manage-api-sketch.md)  
2. **A2A** — [a2a-via-manage.md](../future/a2a-via-manage.md)  
3. Python 仓库收敛为 **仅 Manage** + 从 Node 移除本地 audit JSONL 上传  

---

## 9. 里程碑时间表（建议）

| 里程碑 | 内容 | 依赖 |
|--------|------|------|
| **AC-α** | N0 + N1 | — |
| **AC-β** | N2 | AC-α |
| **AC-γ** | N3 + N4 | AC-β |
| **AC-1.0** | N5 + N6 + N7 | AC-γ |

并行建议：N1 完成后 Client 与 Node 可分人协作；N3 后 Client 重点做 HITL UI。
