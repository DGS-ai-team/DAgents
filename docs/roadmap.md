# DAgents 路线图

> 机器可读版本事实以根目录 [`VERSION`](../VERSION) 为准，变更说明以 [`CHANGELOG.md`](../CHANGELOG.md) 为准。本页只保留当前产品方向、已完成基线和下一阶段优先级。

## 1. 产品定位

DAgents 是本地优先的 Agent 控制台：数据与工具默认留在 Node 所在机器，通过工具权限、policy、审批和审计控制风险；需要跨机协作时使用 Manage Workgroup。

## 2. 当前基线（v0.10.5）

- Go Agent Node：多 Agent、Web UI、HTTP/SSE、Session/Turn/Step、工具、HITL、skills、triggers、临时子 Agent、压缩和媒体产物。
- Node Web UI：消息、上下文、工具审批、终端、浏览器任务、设置和工作组入口。
- Manage：Node/Agent Registry、Workgroup、Node 主动建立的 WS、Console、LLM/Skills/Release/Cases 元数据。
- Workgroup：选择 Node 上已有 Agent 作为成员；每个 `workgroup_id + member_id` 使用独立 Session；工具执行回到成员 home Node。
- 发布：Windows/Linux 安装包和可选桌面托盘；Manage 可选，不是本机对话的前置依赖。

## 3. 优先级

### P0：可靠的本地闭环

1. 首次启动诊断：端口、模型、runtime、policy、Manage 连接的可解释错误。
2. Turn/Step/HITL/异步工具的事件、恢复、取消和 hydrate 一致性。
3. 工具结果的结构化状态、输出截断、媒体引用和错误语义。
4. 真实 LLM、多模态、browser/terminal 的回归用例和低成本 smoke 测试。

### P1：Workgroup 企业化

1. Agent catalog 增量同步、断线 resume、gap reconcile 和连接 fencing 的真实网络演练。
2. Workgroup 成员、Session、Assign、审批和 Timeline 的权威状态在 Manage Console 与 Node UI 对齐。
3. Workgroup 工具 policy overlay、审计脱敏、资源并发和 `indeterminate` 处理完善。
4. Manage 侧低基数指标、运行历史和可导出的审计时间线。

### P2：能力生命周期

- Skills / plugins / external tools 的版本、审批、发布、禁用和 Node 主动同步。
- 触发器的 Webhook、指标来源、死信、幂等去抖和管理 UI。
- 模型路由、成本记账和配额；不改变 Node 对本地 key/工具边界的责任。

## 4. 明确不作为主线

- 通用可视化工作流画布；
- Node-to-Node 直连派活或 Manage 反向访问 Node；
- 通过 Workgroup 隐式创建受限 Agent；
- 把完整 Timeline/raw tool output 广播给所有成员；
- 在没有真实隔离边界前宣传产品级沙箱。

## 5. 参与与变更规则

- 新功能先写当前架构/契约，再实现和补真实验证。
- 日期化分析、一次性实验和版本验收清单放入 [`docs/archive/`](./archive/README.md)。
- 跨组件设计从 [`docs/design/README.md`](./design/README.md) 进入；用户操作从 [`docs/user/README.md`](./user/README.md) 进入。
- 发现实现与文档不一致时，以代码和测试为准，提交修正文档的 PR。

**最后更新**：2026-08-26。
