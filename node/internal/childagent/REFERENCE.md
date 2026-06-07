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
| `FormatChildTask` / `DefaultChildAllowedTools` | 首条 task 与默认工具集 |
| `ParentDelegatableTools` / `resolveAllowedTools` | 工具下放校验 |
| `IsTemporaryAgentTool` / `IsParentOnlyTool` | 临时 Agent 工具分流（非 A2A） |
| `HitlScopeTemporaryAgent` / `EventTemporaryAgent*` | SSE 与 HITL 协议常量 |
