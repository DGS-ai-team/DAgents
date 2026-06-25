# Client（Go TUI）

本地终端 Client：**默认 bubbletea 全屏**（`client/internal/tui/full`），**行模式 REPL 兜底**（`repl/`）。只连接同机 Agent Node。

现代桌面/WSL 仍可用 **[Python Textual TUI](../app/cli/README.md)**（`dagents chat`，位于 `app/cli/tui/`）。选型见 [local-assistant.md](../docs/architecture/local-assistant.md)。

## 目录

| 路径 | 说明 |
|------|------|
| `cmd/dagents-client/` | Client 进程入口 |
| `internal/probe/` | Node 探活（N0） |
| `internal/api/` | Node HTTP/SSE 客户端 |
| `internal/hitl/` | 审批与用户询问（含 `Interact` 回调） |
| `internal/tui/` | 终端入口：`full/` 全屏、`repl/` 行模式、`shared/` 共用 |

版本展示以 Node `GET /health` 为准（`dagents-client version` 探活后输出）；canonical 常量见 `node/internal/version/`。

## 本地运行

与 Node、Python Textual 共用 [`packaging/agent-client/config.yaml`](../packaging/agent-client/config.yaml)（见 [`packaging/agent-client/README.md`](../packaging/agent-client/README.md)）。`-config` 可省略。

```bash
# 探活
go run ./client/cmd/dagents-client probe

# 交互式 TUI（默认 bubbletea 全屏）
go run ./client/cmd/dagents-client tui

# 行模式 REPL（老 SSH / RHEL6）
go run ./client/cmd/dagents-client tui --plain

# 恢复已有 session
go run ./client/cmd/dagents-client tui sess-abc123

# 一次性发送（非交互）
go run ./client/cmd/dagents-client chat "你好"
```

### TUI 模式

| 模式 | 命令 | 说明 |
|------|------|------|
| **full**（默认） | `tui` | bubbletea 全屏；`--show-reasoning` 显示模型推理流 |
| **plain** | `tui --plain` | 行模式；`--show-reasoning` 写 stderr |
| 环境 | `DAGENTS_TUI=plain\|full` | 覆盖自动探测 |

### 斜杠命令（full / plain 共有子集）

| 命令 | 说明 |
|------|------|
| `/status` | agent_id、session_id、队列深度、在途 turn |
| `/sessions` | 列出 session（`*` 为当前） |
| `/switch <id>` | 切换 session |
| `/new` | 新建 session |
| `/clear` | 清空对话上下文 |
| `/context` | 只读 context 视图（含 system_prompt；Esc 返回，full 模式） |
| `/policy` | 工具/shell 策略管理（Esc 返回，full 模式） |
| `/triggers` | 查看已配置触发器（full 模式） |
| `/compress` | 手动触发一次阻塞压缩 |
| `/skill` | 列出 skills；`/skill load\|unload NAME` |
| `/children` | 子 Agent 列表（full 模式） |
| `/history [n\|all]` | 查看最近输出（plain 模式） |
| `/tools verbose\|brief` | tool 输出展开/折叠 |
| `/tools expand\|collapse` | 展开/收起最近 tool 块（full 模式） |
| `/reasoning on\|off` | 运行时切换推理流显示 |
| `/quit` | 退出 |

**取消 turn**：流式输出中难以输入斜杠命令，请用 **`Esc`**（调用 `POST .../cancel`）。

**命令输出**：full 模式下 `/status`、`/sessions`、`/skill`、`/help`、`/children` 以结构化 **system panel** 展示（分区标题、键值对齐、当前 session / 已加载 skill 高亮）。

**滚动**：默认贴底跟随新输出；在 transcript 区 **滚轮上滚** 或按 **PgUp**（输入框为空时 **↑**）可固定阅读位置，流式输出与审批等待期间不会被拽回底部；滚回底部或 **发送消息** 后恢复跟随。

SSE 断线后会按 `Last-Event-ID` 自动重连。

**与 Python Textual Client 对齐的基础逻辑**（`client/internal/tui/shared/turn_gate.go`）：

- `done` 仅语义 B（编排暂停/链结束）；`turn_complete` / `awaiting` 由 Node 下发
- submit 后以 `seqFence` 忽略在途 turn 的陈旧 `done`
- HITL（**`hitl_required`** 展开入队；仍兼容 A2A 的 `approval_required` / `user_information_required`）非阻塞处理；`ask_user_information` 在 transcript 合并为单条「Agent 询问」；暂停态 `done` 正常结束 turn 等待
- 子 Agent turn SSE 过滤（`hitl.ShouldSkipChildRuntimeDisplay`）

## 测试

```bash
go test ./client/...
```
