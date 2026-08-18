# Terminal WebSocket 协议

Node 端的交互式 Terminal 通过以下端点提供：

```text
GET /v1/agents/{agent_id}/terminals/ws
```

一条 WebSocket 连接绑定一个 Agent Terminal。连接建立后，客户端必须先发送 `open`，服务端返回 `started`，随后双方可以交换控制帧和 PTY 输出帧。

## 客户端帧

```json
{"type":"open","target_kind":"local","shell":"powershell","rows":24,"cols":100}
{"type":"input","data":"<base64>"}
{"type":"resize","rows":40,"cols":120}
{"type":"terminate"}
{"type":"close"}
{"type":"ping"}
```

`target_kind` 省略时默认为 `local`；远程 Linux Terminal 使用 `linux_channel`，并在 `target_id` 中填写 channel ID。`data` 使用 JSON `[]byte` 的 base64 表示，避免 PowerShell 控制序列和非 UTF-8 输出被改写。

## 服务端帧

```json
{"type":"started","terminal_id":"local-terminal-1"}
{"type":"output","terminal_id":"local-terminal-1","data":"<base64>"}
{"type":"resized","terminal_id":"local-terminal-1","rows":40,"cols":120}
{"type":"exited","terminal_id":"local-terminal-1","exit":{"code":0}}
{"type":"error","terminal_id":"local-terminal-1","error":"..."}
```

服务端保证同一连接内的 `output` 在 `exited` 前发送完毕。客户端断开后，Node 会在 30 秒宽限期内保留该 Terminal，并继续读取输出。客户端可以使用以下首帧恢复连接：

```json
{"type":"resume","session_id":"local-terminal-1"}
```

The Node Web UI consumes this protocol through the native browser `WebSocket`
API. The first UI slice intentionally avoids a terminal rendering dependency:
it uses a bounded `<pre>` buffer, sends UTF-8 command bytes, preserves the
server's base64 byte framing, reconnects with `session_id`, and surfaces
`replay_gap` instead of silently presenting incomplete output. The UI opens a
terminal only after the user clicks **Connect**; leaving the settings page
closes the client connection while the server keeps the terminal for the
configured reconnect grace period.

恢复时服务端先发送 `started`，再发送有限 replay buffer 中的 `output`（带 `replay: true` 和单调递增的 `seq`）。如果输出超过 1 MiB，会发送 `replay_gap`，客户端应重新建立完整会话或提示用户存在缺口。宽限期结束后会关闭 PTY；当前不支持跨 Node 重启恢复。
