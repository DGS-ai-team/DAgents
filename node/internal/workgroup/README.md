# WorkgroupWorker（D2）

Node 侧工作组成员执行器骨架。权威契约见：

- `docs/design/workgroup-and-node-gateway.md` §11 / §13 D2
- `docs/design/workgroup-d05-contracts.md`

## D2 范围

| 能力 | 状态 |
|------|------|
| WorkerBinding 持久化（非 local agents） | ✅ |
| 幂等 `member.provision` | ✅ |
| Command journal（accept 先落盘再 ack） | ✅ |
| Fencing（lease_epoch / generation / digest / catalog） | ✅ |
| Tool manifest ∩ allowlist | ✅ |
| 会话 `connection_generation` / resume cursor | ✅ 内存骨架 |
| 真实 Manage WS 拨号 / 工具执行 | ⏳ D3 |

## 明确不做

- 不进入 `GET /v1/agents`
- 不装本地 PromptContext / soul
- ACL 不替代 ExecutionGrant
