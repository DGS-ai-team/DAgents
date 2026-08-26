# 工作组指南

工作组用于把多个 Node 上已注册的 Agent 组织成一个可审计的协作单元。Manage 负责组级编排和状态持久化，成员 Agent 在各自 Home Node 上执行工具。

## 1. 前置条件

1. 启动 Manage，并准备管理员登录；
2. 启动一个或多个 Node，完成首次配置；
3. 在 Node 的 Manage 设置中启用 Workgroup；
4. 确认 Node 能主动建立到 Manage 的 WebSocket，Manage 不需要访问 Node 的入站 HTTP。

## 2. 创建工作组

在 Manage Console 或 Node Web UI 中：

1. 创建工作组并配置 Supervisor LLM；
2. 将需要协作的 Node 加入 ACL；
3. 从 Agent 目录选择已有 `agent_id` 作为成员；
4. 发布工作组，等待成员状态变为 ready；
5. 从 Console 或已订阅 Node 打开工作组对话。

成员是 Agent 引用，不是重新创建的受限 Agent。成员当前的模型、工具、skills 和本地工作区仍由 Home Node 管理；工作组只增加协作范围和权限约束。

## 3. 分派方式

| 方式 | 行为 |
|---|---|
| Supervisor 编排 | 用户让 Supervisor 分析任务，Supervisor 使用 `assign_workgroup_task` 分派并汇总 |
| 直达成员 | 在输入中 `@成员显示名`，直接启动成员 turn，不投影为 Supervisor 任务卡片 |

多个用户消息会进入工作组队列；同一成员的运行仍遵守单飞和状态 fencing。任务卡片展示成员、工具清单、运行状态和最终结果。

## 4. 审批与取消

- 成员或 Supervisor 需要用户确认时，审批项进入工作组 HITL 列表；卡片必须关联到具体任务/工具，而不是只显示泛化的“需要审批”。
- 多个工具审批可以逐项处理，也可以在协议允许时批量批准/拒绝；重复提交按 HITL id 幂等处理。
- 取消工作组 turn 会停止编排和成员运行；迟到结果必须被 fencing 丢弃，不能复活已取消任务。
- Timeline 是可恢复事实；`workgroup.realtime` 只用于思考、流式文本和工具运行等临时状态。刷新页面后应从 Timeline/HITL 快照恢复，而不是依赖浏览器内存。

## 5. 成员工具

成员工具以 Node 实际能力和 Workgroup 工具策略的交集为准。默认优先使用文件工具，Shell 需要显式开启并遵守 Node policy。路径使用成员工作区相对路径，不允许通过 `..` 或主机绝对路径越界。

权威工具目录：[shared/workgroup/member_tool_catalog.json](../../shared/workgroup/member_tool_catalog.json)。详细字段和协议见 [Workgroup 契约](../design/workgroup-d05-contracts.md)。

## 6. 断线与排障

| 现象 | 检查 |
|---|---|
| 成员一直 provisioning | Node 是否注册、Manage WS 是否在线、`websockets` 依赖是否安装 |
| 成员离线 | Home Node 进程、ACL、订阅和 Dialer 日志 |
| 任务卡片不更新 | 先看 Timeline，再看 realtime；检查工作组游标是否需要 resume |
| 审批消失后又出现 | 检查 HITL 持久化、重复事件和前端是否使用过期快照覆盖当前状态 |
| 取消后仍有结果 | 检查 assign/command 的 epoch、generation 和 late-result fencing |

实现导航：[架构](../architecture.md)、[Node Workgroup](../../node/internal/workgroup/README.md)、[Manage README](../../manage/README.md)。
