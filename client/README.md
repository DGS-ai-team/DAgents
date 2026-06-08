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
| `internal/version/` | Client 版本号 |

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
| `/cancel` | 取消在途 turn（不退出 TUI） |
| `/context` | 只读 context 视图（含 system_prompt；Esc 返回，full 模式） |
| `/compress` | 手动触发一次阻塞压缩（full / Textual TUI） |
| `/skill` | 列出 skills；`/skill load\|unload NAME` |
| `/history [n\|all]` | 查看最近输出（plain 模式） |
| `/tools verbose\|brief` | tool 输出展开/折叠 |
| `/reasoning on\|off` | 运行时切换推理流显示（也可用 `--show-reasoning` 启动） |
| `/quit` | 退出 |

SSE 断线后会按 `Last-Event-ID` 自动重连。

**与 Python Textual Client 对齐的基础逻辑**（`client/internal/tui/shared/turn_gate.go`）：

- `done` 仅语义 B（编排暂停/链结束）；`turn_complete` / `awaiting` 由 Node 下发
- submit 后以 `seqFence` 忽略在途 turn 的陈旧 `done`
- HITL（`approval_required` / `user_information_required`）非阻塞入队；`ask_user_information` 在 transcript 合并为单条「Agent 询问」；暂停态 `done` 正常结束 turn 等待
- 子 Agent turn SSE 过滤（`hitl.ShouldSkipChildRuntimeDisplay`）

## 测试

```bash
go test ./client/...
```
