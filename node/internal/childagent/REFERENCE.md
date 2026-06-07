# REFERENCE — `node/internal/childagent`

架构与流程说明见 [`README.md`](./README.md)。

| 符号 | 说明 |
|------|------|
| `Manager` | 子 Agent 记录表、Create/Cancel/Wait、SSE、resume 路由 |
| `Config` | TTL、并发上限、`default_wait_timeout_seconds` 等 |
| `Host` | `session.Manager` 实现的 spawn/stop/resume 宿主 |
| `SpawnSpec` | 创建子 runtime 参数 |
| `ActiveAgent` / `Result` | 活跃账本与终态交付 |
| `newActiveAgent` | 创建活跃账本条目 |
| `unregisterActive` / `waitUntilSettled` | 移出活跃表 / wait=true 阻塞 |
| `CreateInput` | `create_temporary_agent` 解析后入参 |
| `Status` / `Status*` | 生命周期状态常量 |
| `HandleCreate` | 实现 `create_temporary_agent` |
| `HandleWait` / `HandleStatus` / `HandleCancelTool` | wait / status / cancel 工具 |
| `HandleParentTool` | orchestrator 统一入口 |
| `OnChildSettled` | 子 runtime 空闲完成时回调 |
| `RouteResume` | 父 session resume 路由到父或子 runtime |
| `CancelAllForParent` | 父 session 释放时级联取消 |
| `RelayHub` | 子 turn SSE → 父 session |
| `RestrictedRegistry` | 工具白名单，实现 `tools.Executor` |
| `FormatChildTask` / `DefaultChildAllowedTools` | 首条 task 与默认工具集 |
| `ParentDelegatableTools` / `resolveAllowedTools` | 工具下放校验（`parse.go`） |
| `IsTemporaryAgentTool` / `IsParentOnlyTool` | 临时 Agent 工具分流（非 A2A） |
| `HitlScopeTemporaryAgent` | HITL scope 常量 `temporary_agent` |
| `EventTemporaryAgentCreated` 等 | SSE 生命周期事件名 |
| `ToolCreateTemporaryAgent` 等 | 管理工具名常量 |
| `ToolLoadSkills` 等 | skills 工具名（仅父 Agent，不可下放） |
