# Workgroup Agent Membership v1 Smoke Checklist

## 网络方向

- [ ] Node 能主动建立到 Manage 的 WSS。
- [ ] Node 不开放给 Manage 访问的 HTTP/WS 端口。
- [ ] Manage 无法向 Node 发起新 HTTP 请求，但已有 WS 可正常下发命令。
- [ ] Node 断线后能主动重连并恢复游标。

## Agent 注册和绑定

- [ ] 一个 Node 注册多个已有 Agent。
- [ ] Agent 新建、修改、归档能同步到 Manage。
- [ ] Workgroup 可以选择已有 Agent，不产生新的受限 Agent。
- [ ] Agent 离线时成员进入等待状态，而不是错误 ready。
- [ ] Agent 恢复连接后 Session 可以建立或恢复。

## Session 隔离

- [ ] 同一 Agent 的个人 Session 与 Workgroup Session 消息不串。
- [ ] 同一 Agent 加入两个 Workgroup 时历史和工具结果不串。
- [ ] 同一 Session 串行，不同 Session 可以并行。
- [ ] terminal、文件、异步回调、HITL 和 cancel 都按 Session 隔离。

## WS 可靠性

- [ ] 重复 `agent.session.open` 幂等。
- [ ] 重复 `agent.turn.start` 不重复执行。
- [ ] 旧 connection generation 的事件被拒绝。
- [ ] 断线后可靠事件只出现一次。
- [ ] 未知副作用 turn 显示 `indeterminate`，不自动重跑。

## UI

- [ ] 添加成员弹窗展示 Agent 选择器，不再展示 Home Node 创建表单。
- [ ] 成员列表展示 Agent、Node、Session 和 Turn 分层状态。
- [ ] offline/binding/ready/error/indeterminate 状态有明确文案。
- [ ] 个人消息页和工作组消息页互不混入。
- [ ] Manage Console 与 Node UI 均能完成成员选择、运行和取消。

## 真实联调

- [ ] 启动一个 Manage 和至少两个 Node。
- [ ] 使用真实 Agent 配置完成一次工作组任务。
- [ ] 使用真实工具和终端完成一次任务。
- [ ] 运行中断开 Node 网络，重连后恢复。
- [ ] 使用真实 LLM 验证 Agent 自身 prompt、skills、tools 和模型配置仍生效。

## v0.10.0 本轮实测记录（2026-08-24）

| 项目 | 结果 | 说明 |
| --- | --- | --- |
| Node → Manage 出站 WS | 通过 | 本地双进程使用 Node 主动建立 WS；未做 TLS/WSS 部署验证。 |
| Manage 反向访问 Node | 通过 | AgentRef 控制消息均通过已建立 WS 下发；未增加反向 HTTP。 |
| 一个 Node 注册多个 Agent | 通过 | Registry 中可看到多个本地 Agent 记录。 |
| Workgroup 选择已有 Agent | 通过 | 创建后成员进入 `ready`，未创建新的受限 Agent。 |
| 两个 Workgroup 并行使用同一 Agent | 通过 | 两个独立历史文件只包含各自消息，未发生串话。 |
| AgentRef 真实消息闭环 | 通过 | Manage + Node 独立进程、mock LLM 完成创建、发送、事件和最终结果。 |
| 重复 start / 最终文本竞态 | 通过 | Go 单测覆盖幂等；真实联调覆盖最终结果非空。 |
| AgentRef 归档与 session close | 通过 | Python 单测覆盖 close outbox 及迟到 close 不复活已归档成员；Go 单测覆盖 close 控制帧 delivery ACK。 |
| resume 与实时 outbox 并发投递 | 通过 | Manage 连接级发送锁 + Node 低序 ACK 幂等处理；重新构建后两工作组并发消息无断线。 |
| AgentRef cancel 协议 | 通过 | Python vertical 单测覆盖 cancel 下发、waiter 唤醒和 late-result fencing；mock LLM 未提供稳定的长运行窗口。 |
| Node UI Agent 选择 | 通过 | 真实浏览器完成目录加载、已有 Agent 选择、表单校验和取消。 |
| 工作组页实时事件状态 | 通过 | NavRail 接收工作组页自身 EventSource 的权威状态，并改用“实时事件”文案；实测工作组页显示“在线”。 |
| Manage Console 交互 | 部分 | 构建和路由通过；隔离环境无管理员凭据，未完成登录态点击验收。 |
| 真实 LLM / 真实工具 / 终端 | 未覆盖 | 需要下一轮使用可控的真实配置和测试主机，避免把 mock 结果当成能力证明。 |
| 断网重连 / HITL / WSS | 未覆盖 | 保留为发布前的专项验收项。 |
