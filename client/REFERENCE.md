# REFERENCE — `client`

## `cmd/dagents-client/main.go`

| 符号 | 说明 |
|------|------|
| `main` / `run` | 子命令分发（`probe`、`chat`、`tui`） |
| `cmdProbe` | 加载配置并调用 `probe.Node` |
| `cmdChat` | 无参数进入 TUI；有参数则一次性 chat |
| `cmdTUI` | 交互式 TUI（默认 full；`--plain` 行模式）；可选 session_id |

## `internal/probe/probe.go`

| 符号 | 说明 |
|------|------|
| `Result` | 探活结果摘要 |
| `Node` | GET `/health` + `/v1/agent/info` |

## `internal/api/client.go`

| 符号 | 说明 |
|------|------|
| `Client` | Node HTTP 客户端 |
| `StreamEvent` | 解析后的 SSE 事件 |
| `(c *Client) CreateSession` | POST `/v1/sessions` |
| `(c *Client) SubmitMessage` | POST `/v1/messages` |
| `(c *Client) SubmitResume` | POST `/v1/messages` resume |
| `(c *Client) CancelTurn` | POST `/v1/sessions/{id}/cancel` |
| `(c *Client) StreamEvents` | GET `/v1/streams` 并回调 |
| `(c *Client) GetAgentInfo` | GET `/v1/agent/info` |
| `(c *Client) ListSessions` | GET `/v1/sessions` |
| `(c *Client) GetSessionContext` | GET `/v1/sessions/{id}/context` |
| `(c *Client) ClearSessionContext` | POST clear-context |
| `(c *Client) DeleteSession` | DELETE session |

## `internal/hitl/interact.go`

| 符号 | 说明 |
|------|------|
| `Sink` | SSE 输出回调（含 `OnCompression`） |
| `Interact` | HITL 回调（全屏 TUI）；nil 时 stdin |
| `HandleStreamEvent` | 处理事件与 HITL；支持 `context_compression_*` |
| `BuildApprovalSelectionResume` | 逐条勾选审批 resume |
| `ExpandHITLRequired` | 将 `hitl_required` 展开为 user_info / approval 队列项 |
| `ApprovalQueueKey` | HITL approval 队列去重键（含 `hitl_id` 回退） |
| `ExtractUserInformationRequest` | 解析 ask_user_information |
| `FormatUserInformationTranscriptLines` | 合并「Agent 询问」transcript 行 |
| `PrintUserInformationTranscript` | REPL 输出合并询问块 |
| `BuildUserInformationResumeFromOptions` | 选项式回答 resume |
| `FormatContextCompression` | 压缩 SSE 格式化为终端提示行 |

## `internal/tui/`

见 [`internal/tui/REFERENCE.md`](internal/tui/REFERENCE.md)：`dispatch.Run`、`full/`、`repl/`、`shared/`。

版本号以 Node `GET /health` 为准（`dagents-client version` 探活后输出）；canonical 常量见 `node/internal/version/`。
