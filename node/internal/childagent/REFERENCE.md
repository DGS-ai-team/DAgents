# REFERENCE — `node/internal/childagent`

| 符号 | 说明 |
|------|------|
| `Manager` | 子 Agent 记录表、Create/Cancel/Wait、SSE |
| `Config` | TTL、并发上限等 |
| `Host` | session.Manager 实现的 spawn/stop/resume 宿主 |
| `SpawnSpec` | 创建子 runtime 参数 |
| `Record` / `Result` | 子 Agent 元数据与终态 |
| `RelayHub` | 子 turn SSE → 父 session |
| `RestrictedRegistry` | 工具白名单 |
| `LookupTemplate` / `FormatTask` | 模板与首条 task |
| `IsChildAgentTool` / `IsParentOnlyTool` | 工具分流 |
