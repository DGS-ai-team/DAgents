# WorkgroupWorker（D2 + D3 工具执行）

Node 侧工作组成员执行器。权威契约见：

- `docs/design/workgroup-and-node-gateway.md` §11 / §13 D2–D3
- `docs/design/workgroup-d05-contracts.md`

## 范围

| 能力 | 状态 |
|------|------|
| WorkerBinding 持久化（非 local agents） | ✅ |
| 幂等 `member.provision` | ✅ |
| Command journal（accept 先落盘再 ack） | ✅ |
| Fencing（lease_epoch / generation / digest / catalog） | ✅ |
| Tool manifest ∩ allowlist | ✅ |
| 会话 `connection_generation` / resume cursor | ✅ 内存骨架 |
| `read_file` 工作区执行 | ✅ D3 |
| accepted 重启恢复（不双执行） | ✅ D3 |
| Manage WS hub + Node envelope dispatch | ✅ D3 骨架（`ws_hub` / `DispatchEnvelope`） |
| 生产级 WebSocket 长连接拨号 | ⏳ 后续 |

## 明确不做

- 不进入 `GET /v1/agents`
- 不装本地 PromptContext / soul
- ACL 不替代 ExecutionGrant
