# WorkgroupWorker REFERENCE

## 包职责

| 文件 | 职责 |
|------|------|
| `types.go` | D0.5 实体（Binding / Command / Ack / Envelope） |
| `digest.go` | Canonical JSON + `sha256:` |
| `fencing.go` | lease_epoch / generation / digest / catalog |
| `binding.go` | WorkerBinding 存储（memory / dir） |
| `journal.go` | command journal（accept 先落盘） |
| `provision.go` | 幂等 `member.provision` |
| `manifest.go` | tool catalog ∩ allowlist |
| `command.go` | tool.command accept / 拒绝 / 不重执行 |
| `session.go` | connection_generation / resume cursor |
| `worker.go` | 聚合入口 |

## 与本地 Agent 隔离

`WorkerBinding.not_enumerable_as_local_agent=true`。成员不得出现在 `GET /v1/agents`；D3 接线时由 API 层显式过滤。

## 下一阶段（D3）

- Manage 真实 WS 服务端 + outbox
- Node WS dial / resume gap-fill
- 真实 Manage WS 拨号（D3 用可注入 bridge / 单测 FakeNodeBridge）
