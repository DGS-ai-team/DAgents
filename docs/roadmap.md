# DAgents 路线图

> 机器可读版本事实以根目录 [`VERSION`](../VERSION) 为准，变更说明以 [`CHANGELOG.md`](../CHANGELOG.md) 为准。本页只保留当前产品方向、已完成基线和下一阶段优先级。

## 1. 产品定位

DAgents 面向个人使用者：Windows Desktop 提供交互，自己的 Node 持有数据、会话并持续执行任务；可通过自建 SSH 隧道连接远端 Node。多个 Desktop 共享同一使用者的业务事实，当前不引入多人账号、私人上下文或租户隔离。Manage 是可选的跨节点工作组控制面。

长期任务及反馈闭环的单 Node MVP 已随 v0.11.0 完成发布；已发布版本基线见下节。

## 2. 已发布基线（v0.11.0）

本节是上次正式发布的范围；当前开发线的 Desktop、API-only Node 和配置归属以[发布收束清单](release-readiness.md)为准。

- Go Agent Node：多 Agent、Web UI、HTTP/SSE、Session/Turn/Step、工具、HITL、skills、triggers、临时子 Agent、压缩和媒体产物。
- Node Web UI：消息、上下文、工具审批、终端、浏览器任务、设置和工作组入口。
- Manage：Node/Agent Registry、Workgroup、Node 主动建立的 WS、Console、LLM/Skills/Release/Cases 元数据。
- Workgroup：选择 Node 上已有 Agent 作为成员；每个 `workgroup_id + member_id` 使用独立 Session；工具执行回到成员 home Node。
- 发布：Windows/Linux 安装包和可选桌面托盘；Manage 可选，不是本机对话的前置依赖。

## 3. 当前开发线与下一版

下一版建议为 v0.12.0，进入功能冻结和候选包验收，不再叠加新的产品系统。完整状态、证据和未完成项集中维护于[发布收束清单](release-readiness.md)。

### P0：形成可安全安装、迁移和恢复的发行版

1. 关闭模型配置引用与密钥保存行为，保护 MCP 配置往返和草稿。
2. 验收最终 Windows 包的标准用户初装、快捷方式/自启、运行中升级、卸载、备份恢复及 v0.11.0 数据迁移。
3. 对最终候选完成日常对话、资源、审批、工具失败收尾、空闲后新上下文与 Auto/Dreaming 的恢复和耐久测试。
4. 真实 SSH 隧道、Linux Node 与打包 Browser/Chromium 进行环境验收；下一版 Windows 仅发 x64 完整包，暂停 x86。
5. 统一版本元数据、变更说明、支持矩阵、依赖审计和更新安装链路，按 dev → main 的发布 PR 流程交付。

### P1：体验与可维护性收尾

- 小窗口、DPI、键盘操作和减少动画偏好；不再次重写已确认的视觉系统。
- 真实 MCP、反馈到 Manage、工作组审批/取消/恢复的组合回归。
- 当前文档入口与历史设计快照分开；重复/退役代码按实际使用证据清理，不以行数作为删功能的依据。

### P2：有独立需求后再立项

- 浏览器人工接管与可交互嵌入式页签。
- Desktop 自建 SSH 隧道；现阶段继续由用户建立加密转发。
- macOS/Linux Desktop、多用户/租户隔离和 LAN/WAN 直连鉴权。
- 更广泛的事件源、工作流画布、插件发布生态与成本计账。

以上均不是本版稳定发布的前置功能。安全或数据一致性缺陷随时提升为 P0。

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

**最后更新**：2026-10-17。
