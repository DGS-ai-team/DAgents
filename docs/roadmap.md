# DAgents 路线图

> 机器可读版本事实以根目录 [`VERSION`](../VERSION) 为准，变更说明以 [`CHANGELOG.md`](../CHANGELOG.md) 为准。本页只保留当前产品方向、已完成基线和下一阶段优先级。

## 1. 产品定位

DAgents 面向组织自有 Windows/Linux 机器，提供本地 Agent 执行、人工治理和跨机协作。数据、工具和最终执行权保留在 Node；长期任务通过受控唤醒分次推进，跨机协作使用可选的 Manage Workgroup。

长期任务及反馈闭环的单 Node MVP 已在当前工作区验收，尚未发布；已发布版本基线见下节。

## 2. 当前基线（v0.10.7）

- Go Agent Node：多 Agent、Web UI、HTTP/SSE、Session/Turn/Step、工具、HITL、skills、triggers、临时子 Agent、压缩和媒体产物。
- Node Web UI：消息、上下文、工具审批、终端、浏览器任务、设置和工作组入口。
- Manage：Node/Agent Registry、Workgroup、Node 主动建立的 WS、Console、LLM/Skills/Release/Cases 元数据。
- Workgroup：选择 Node 上已有 Agent 作为成员；每个 `workgroup_id + member_id` 使用独立 Session；工具执行回到成员 home Node。
- 发布：Windows/Linux 安装包和可选桌面托盘；Manage 可选，不是本机对话的前置依赖。

## 3. 优先级

### P0：自主任务上线前的执行与访问基础（已验收，未发布）

1. **Manage 默认鉴权**：保护接口不再授予匿名 admin；移除默认管理员密码；Node 登录和 Workgroup WS 绑定认证身份；旧不安全会话失效，提供明确升级配置说明。
2. **触发器执行归属与权限**：目标必须路由到已有 Agent 的实际运行时，校验会话所属关系；停止任意宿主 shell 条件门控的默认执行路径。
3. **触发投递安全**：并发原子领取、投递身份持久化、落盘失败不派发；重启未决投递需要对账，无法确认时暂停并提供人工恢复入口，不自动重放副作用。
4. **基础 UI 闭环**：启动失败可重试并查看帮助；定时任务明确目标 Agent，提供立即运行和触发历史，区分已投递、失败与待核对。

实施边界与验收见 [P0 执行与访问基础](design/p0-controlled-execution-foundation.md)。以上是本轮范围，不包含完整长期目标、反馈系统、SSO/RBAC 或通用 OS sandbox。

### P1：反馈闭环与单 Node 自主长期任务（MVP 已验收，未发布）

1. **反馈先交付**：Node 本地保存与重试 → 当前连接 Manage 的管理员查看、处理、回复 → Node 主动同步状态；身份隔离、去重与更换 Manage 的归属保护一起验收。
2. **长期目标 MVP**：目标与完成条件、单次 Run/Turn 关联、进度证据、受控下一次唤醒、累计预算、暂停/停止、审批等待和必要通知。
3. **任务 UI**：在现有 Agent 工作区提供长期任务详情、运行时间线、产物与待处理入口，保持原对话和终端布局。
   当前入口已调整为独立 Auto Agent 类型的智能体设置；旧 Goal 页面保留历史。任务终态后复用同一 Agent 开启新周期尚未实现。
4. **场景验收**：三类合成文件目标通过真实 LLM 验证；取消、审批恢复、并发和重启采用确定性回归。长期运行及 browser/terminal 外部故障演练仍属扩大验证范围。详细证据和边界见 [P1 验收记录](design/p1-feedback-and-autonomous-goals.md)。

### P2：受控多机协作与运维

进入扩大多机投入前，优先补齐 Auto 任务周期复用、持续观察的无变化语义、Node 结果与待处理汇总、长时故障演练；这些是后续规划。定位与排序见[普通/Auto 产品评估](comparative-analysis/2026-09-08-normal-and-auto-product-assessment.md)。

1. Workgroup catalog、resume、gap reconcile、连接 fencing 和重复投递的真实网络演练。
2. Manage Console 与 Node UI 对齐成员、Session、Assign、审批、Timeline 和任务状态。
3. 工具 policy overlay、审计脱敏、资源并发与未知副作用处理；控制面只能收紧本地权限。
4. doctor/脱敏支持包、版本兼容矩阵、升级备份与回滚；按支持场景建设设备身份、RBAC、Secret Store 和 OS sandbox。

### P3：能力生态与规模化产品能力

- Skills / plugins / external tools 的版本、审批、发布、禁用、回滚和 Node 主动同步。
- Webhook、事件条件、死信、模型路由、成本记账和配额。
- 稳定 SDK/Provider/Recipe 契约、发布签名和外部案例。

验证发现的安全或数据一致性缺陷随时提升为 P0，不受以上功能排期限制。

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

**最后更新**：2026-09-08。
