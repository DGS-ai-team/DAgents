# Auto 与工作组验收记录（2026-10-12）

本记录区分 Go 回归与原生 Desktop 实测。主 Node `18765` 的运行数据与 Agent 配置未改，主 Desktop 未重启；其前端 HMR 反映了本轮 UI 修复。测试使用隔离 Node `18766`、内存 Manage `8021` 和 Desktop 原生窗口。内存 Manage 仅在隔离进程中复用已授权的 `mimo-v2.5-pro` profile。期间曾在一次工具输出中误显示隔离 Node 的临时 Manage token；随后旧 helper 退出、设置恢复并生成新 token，先前 token 已失效。没有涉及真实模型 API key 或主服务数据；最终 token 未写入报告。测试进程清理由 root 完成。

## Auto 原生链路

专用 Auto Agent 为 `agt-b906f86c8ba50c35`，私有 workspace。先关闭自动唤醒与 Dreaming，原生写入种子事实 `27×13=351` 和 marker `AUTO_DREAM_FACT_351`，再把 trigger 间隔设为 60 秒。Node 调度器实际触发多轮真实 Mimo 回合；todo_create 与 todo_update 均在原生界面审批并成功。trigger 已关闭，wake 为 0；该阶段的第三次触发发生在审批等待期间，最终队列为空。

随后由真实分钟调度器执行 Dreaming。证据为 experience revision 1、model-visible context 清空为 0、turn idle、队列为空；durable transcript 保留 14 条。下一条普通回合的 system prompt 包含新经验，模型据此复述 marker 和 `expected_revision`，没有调用工具。Durable 历史仍可读是预期行为；验收依据是 active context 清空和下一回合读到经验。

新 Dreaming prompt 增加了权限边界：摘要只记录已验证事实与可复用流程；一次审批只授权当次明确操作，不扩大未来权限；不得推断或编写绕过用户限制、拒绝、撤销或审批规则的建议。该规则在本轮代码更新后加入，**不代表旧 revision 1 摘要由新 prompt 生成或被自动重写**。

## 工具状态投影修复

真实 todo_create 经批准后，Todo 记录的业务字段为 `status=pending`，但工具执行已成功。通用结果分类器此前把裸业务字段误当成执行状态，hydrate 后显示 `awaiting_user`。修复保留通用分类，不按 todo 工具名单独特判：裸 JSON 中的 `pending` 与 `in_progress` 不再映射到执行 awaiting/running；明确 `running` 仍有效，显式 `detail.status` 执行 envelope 可识别 `in_progress`，HITL 的 `awaiting_user`、`awaiting_hitl`、`awaiting_approval` 继续有效。

Go 回归覆盖 Todo 的 `pending`、`in_progress`、`completed` 三种业务状态均投影为 `succeeded`，同时验证明确运行状态与 HITL 状态。`go test ./internal/tools ./internal/api -count=1`、`go test ./internal/session ./internal/turn -count=1` 均通过；隔离 Node v2 构建成功。

原先已持久化的 Auto todo_create 消息仍保留旧 `ToolResultMeta.status=awaiting_user`，本轮不做历史迁移。修复后另一次真实原生 todo_create 经批准通过，气泡显示已完成，API 显示 `execution_status=succeeded`、Todo 业务状态仍为 `pending`。原生证据见 [11-new-todo-tool-succeeded.jpg](./11-new-todo-tool-succeeded.jpg)。

随后原生 todo_update 把业务状态改为 `in_progress`，经批准后气泡仍显示工具已完成；API 的执行状态为 `succeeded`，Todo 状态为 `in_progress`。证据见 [13-in-progress-todo-tool-succeeded.jpg](./13-in-progress-todo-tool-succeeded.jpg)。

## 工作组真实模型验收

隔离 Manage 的内存 supervisor profile 名为 `isolated-mimo-supervisor`。专用普通 worker 为 `agt-f29040125cca0198`，绑定 `mimo-v2.5-pro`、私有 workspace，工具组为空，skills 和 child agents 关闭；没有复用 Auto 或已有 Desktop 实测 Agent。

第一次添加成员返回 `403 not_authorized`。这是测试夹具缺少 Manage Registry `discovery_group` 前置配置，不是生产授权缺陷：添加成员要求调用 Node 与目标 Agent 的 Home Node 在 Registry 中共享至少一个 discovery group；Node token 的组可见范围不会代替 Registry 记录分组。已在隔离 Manage 通过管理员 API 建立 `desktop-e2e-20261012`，且只分配给隔离 Node `6602d7d4-084a-477c-bdd6-5f347b5b664b` 与该 worker。

配置该前置条件后，root 原生创建并发布工作组 `wg_3a4a5cdcbe0e159a86027ff79c`，成员 `mb_ca2abee643d49365a4e6be7dcd`（Agent `agt-f29040125cca0198`）状态 ready，随后订阅工作组并发送真实 supervisor 任务。Node/Manage API 显示 supervisor profile `isolated-mimo-supervisor` 解析为 live `mimo-v2.5-pro`。Leader run `rn_349736fe9c61800ffee8677f8a` 为 succeeded；Timeline 包含 `assign_started` 与 `assign_finished`，二者同属 `as_99fcc67702654d804032587c79`；worker actor 返回 `323`，Supervisor 最终文本包含 marker `WORKGROUP_REAL_OK_323`。这是一次实际 Supervisor→worker→Supervisor 汇总回合。工作组创建、成员 ready、发布和订阅的原生证据见 [14-workgroup-published-subscribed.jpg](./14-workgroup-published-subscribed.jpg)。

此必需条件已补入[工作组用户指南](../../../../../user/workgroups.md)。

## Desktop 尺寸检查

root 原生检查了最大化尺寸 `2560×1392` 与还原尺寸 `1183×763`；边缘拖拽缩小尚未实测，因此不将小屏/缩放全项标记通过。Auto 与工作组相关截图按编号保存在本目录。

## Desktop 导航与历史复验

root 原生复验了同一 Auto Agent 的导航：进入工作组后，点击侧栏中当前 Auto 会返回 Auto 聊天，资源计数为 0；证据见 [19-same-agent-sidebar-returns-chat.jpg](./19-same-agent-sidebar-returns-chat.jpg)。再次进入工作组后，唯一用户消息、worker 的 `323` 和 Supervisor marker 均可从历史恢复，见 [18-workgroup-history-restored.jpg](./18-workgroup-history-restored.jpg)。[17-sidebar-same-agent-before-fix.jpg](./17-sidebar-same-agent-before-fix.jpg) 是修复前失败过程，不能作为通过证据。

修复将显式侧栏选择与同 Agent 的事件刷新区分开：侧栏点击进入聊天；SSE hydrate 刷新继续保留当前文件、浏览器或终端视图。回归覆盖同 Agent 事件刷新、明确侧栏点击和切换 Agent。`desktop/tray-tauri` 中 `npm run test:ui` 为 42/42 通过，`npm run build` 成功；构建有既有动态导入与大分块提示。

## 清理与尚未覆盖

本轮专用 Auto 与 worker 已归档（API 返回 404），隔离 Workgroup subscription 已 DELETE 取消，测试 Desktop 已停止。内存 Manage 与 launcher/helper 已停止，8021 不再监听；临时 launcher 源文件移入被忽略的 `.runtime-test-desktop-real`，不进入交付代码。helper 未通过 stdin `restore` 正常退出，导致其内存中的原 Manage 快照丢失；root 随后清除了隔离 Node 上本轮临时 Manage 连接与 token，并禁用了测试 Manage（workgroup 也关闭），而非声称精确恢复了原快照。隔离 Node `18766` 已停止，18766/8021 均无监听；主 Node、Vite 与主 Desktop 未停止或修改运行数据。

边缘拖拽缩放、小屏输入栏、设置项完整保存与运行生效、拒绝审批/询问信息/记忆冲突、人工接管浏览器、地址栏导航、打包与辅助 bridge、所有外部服务及长期时序仍未覆盖。工作组与 Auto 的本轮通过范围仅限本报告列明的真实回合和界面路径。

## 原生截图索引

- [01-auto-human-seed.jpg](./01-auto-human-seed.jpg)：Auto 种子事实。
- [02-auto-wake-config-saved.jpg](./02-auto-wake-config-saved.jpg)、[03-auto-trigger-working-overview.jpg](./03-auto-trigger-working-overview.jpg)：真实 wake 配置与运行状态。
- [04-auto-trigger-approval.jpg](./04-auto-trigger-approval.jpg)、[06-auto-todo-completed.jpg](./06-auto-todo-completed.jpg)：工具审批与 Todo 完成。**[05-auto-completed-tool-status-before-fix.jpg](./05-auto-completed-tool-status-before-fix.jpg) 是修复前错误显示的缺陷证据，不是通过截图。**
- [07-dreaming-config-enabled.jpg](./07-dreaming-config-enabled.jpg)、[08-dreaming-summary-in-history.jpg](./08-dreaming-summary-in-history.jpg)、[09-experience-read-next-turn.jpg](./09-experience-read-next-turn.jpg)：Dreaming 调度、摘要持久化与下一回合读取经验。
- [10-maximized-chat-layout.jpg](./10-maximized-chat-layout.jpg)：最大化聊天布局；还原尺寸与最大化尺寸均检查，边缘拖拽和缩放仍未覆盖。
- [11-new-todo-tool-succeeded.jpg](./11-new-todo-tool-succeeded.jpg)、[13-in-progress-todo-tool-succeeded.jpg](./13-in-progress-todo-tool-succeeded.jpg)：修复后的真实 Todo 创建和 in-progress 更新工具状态。
- **[12-workgroup-member-discovery-error.jpg](./12-workgroup-member-discovery-error.jpg) 是隔离夹具缺少共享 discovery group 的前置条件错误，非产品权限缺陷，也不是通过截图。**
- [14-workgroup-published-subscribed.jpg](./14-workgroup-published-subscribed.jpg)、[15-workgroup-real-delegation-completed.jpg](./15-workgroup-real-delegation-completed.jpg)、[16-workgroup-final-summary.jpg](./16-workgroup-final-summary.jpg)：工作组发布/订阅、真实 worker 委派完成和 Supervisor 汇总结果。
- [17-sidebar-same-agent-before-fix.jpg](./17-sidebar-same-agent-before-fix.jpg)：同 Agent 点击未离开工作组的修复前失败过程，不是通过证据。
- [18-workgroup-history-restored.jpg](./18-workgroup-history-restored.jpg)、[19-same-agent-sidebar-returns-chat.jpg](./19-same-agent-sidebar-returns-chat.jpg)：修复后的工作组历史恢复及侧栏点击回到同 Agent 聊天。
