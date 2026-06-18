# manage — Node 向 Manage 出站 sidecar

| 组件 | 说明 |
|------|------|
| `registrar.go` | 注册 / 心跳 / 注销（含 **Agent Card** 上报） |
| `agentcard.go` | 加载 `agent-card.json` |
| `inbox_poller.go` | A2A inbox long poll |
| `compliance_executor.go` | 合规助手 inbox 处理（turn loop；`custom.md` 由 prompt 侧车注入） |
| `task_replier.go` | inbox Task ack/reply 共用 HTTP |

符号索引见 [REFERENCE.md](./REFERENCE.md)。
