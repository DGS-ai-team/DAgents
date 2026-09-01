# `node/internal/childagent` 符号索引

完整流程见 [`README.md`](./README.md)。

| 符号 | 说明 |
|---|---|
| `Manager` | 生命周期账本、同步创建、取消、TTL、快照、SSE 和 resume 路由 |
| `Config` | 启用开关、TTL、最大回合数和父级并发上限 |
| `Host` | `session.Manager` 实现的 spawn/stop/task/resume 宿主 |
| `RunRepository` | ChildRun 快照持久化边界 |
| `SpawnSpec` | 创建子 runtime 所需的参数 |
| `ActiveAgent` / `ActiveAgentSnapshot` | 运行态对象与一致性快照 |
| `Progress` | 父 Agent/UI 可见的轻量进度投影 |
| `Result` | 同步 create 返回的终态结果 |
| `RunRecord` | repository 使用的控制面持久化记录 |
| `HandleCreate` | `create_temporary_agent` 同步工具实现 |
| `HandleCancelTool` | `cancel_temporary_agent` 工具实现 |
| `HandleParentTool` | orchestrator 的父工具统一入口 |
| `OnChildSettled` | 子 runtime 正常完成回调 |
| `OnChildFailed` | 子 runtime 异常退出回调 |
| `RouteResume` | 将父 session 的 resume 路由到父或子 runtime |
| `CancelAllForParent` | 父 session 释放时级联取消 |
| `ListSnapshots` | 合并运行态、内存终态和持久化终态的读取入口 |
| `ObserveChildEvent` | 从 RelayHub 事件更新进度并持久化 |
| `finishWithEvent` | 所有终态的统一收敛函数 |
| `RelayHub` | 子 runtime SSE 转发到父 session |
| `RestrictedRegistry` | 子 runtime 工具白名单执行器 |
| `FormatChildTask` | 首条 task 的固定包装 |
| `DefaultChildAllowedTools` | 默认可下放工具集 |
| `ParentDelegatableTools` / `resolveAllowedTools` | 工具下放校验 |
| `IsTemporaryAgentTool` / `IsParentOnlyTool` | 工具分流和 parent-only 判定 |
| `Status*` | `creating`、`active` 及各终态常量 |
| `EventTemporaryAgent*` | 创建、进度、完成、取消事件名 |
| `ToolCreateTemporaryAgent` / `ToolCancelTemporaryAgent` | 当前对外管理工具名 |
