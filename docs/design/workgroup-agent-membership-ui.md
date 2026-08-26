# Workgroup Agent Membership UI

## UI 目标

工作组成员页展示“已有 Agent 的引用和运行状态”，而不是“创建一个受限成员”。

## 添加成员

成员弹窗应提供：

- Agent 搜索：名称、ID、Node；
- 按 Node 分组；
- Agent 在线、离线、已归档状态；
- 工具/技能/模型能力摘要；
- 使用权限和当前工作组数量；
- 工作组显示名和角色；
- 只能收紧工具策略。

移除旧的 Home Node 手工选择、成员独立 prompt、成员独立 LLM 和“配置到 Home Node”表述。

## 成员列表

同时显示：

- 工作组显示名和原 Agent 名称；
- 所在 Node；
- Agent 状态；
- Session 状态；
- 当前 turn 状态；
- 最近权威事件时间；
- 重试、替换、归档操作。

`busy` 不再作为唯一状态。成员卡片应明确区分 `waiting_for_node`、`binding`、`ready`、`running`、`awaiting_hitl`、`indeterminate` 和 `archived`。

## 消息页

- Workgroup Timeline 只展示工作组可见事件和成员产出；
- Agent 个人消息页不展示 Workgroup Session；
- 成员详情默认展示最终产出，工具和运行细节按 Assign 展开；
- 取消按钮携带当前 `turn_id`，不能取消其他 Session；
- UI 状态只由 `agent.session.*`、`agent.turn.*` 和 Timeline 权威事件驱动。

## Manage Console 与 Node UI

Manage Console 负责 Agent Registry 和成员选择；Node UI 负责本地 Agent 的个人 Session 和本 Node 工作组 Session 展示。两者均不得要求 Manage 反向访问 Node。

## v0.10.4 已落地 UI

- Node UI 的工作组成员弹窗改为从 `/v1/workgroups/meta/agents` 加载已有 Agent，按 Agent 名称、ID、Node 展示并要求选择 `agent_id`。
- Manage Console 的工作组成员添加流程同样提交 `agent_id`，过滤承载 Node 自身的兼容注册行，避免把 Node 误显示为可加入的 Agent。
- 新建 AgentRef 成员不再填写 Home Node、成员独立 prompt、成员独立模型或工具清单；这些运行时属性来自所选 Agent。
- 成员列表和消息页保留现有工作组状态投影；`binding`、`waiting_for_node`、`awaiting_hitl` 等细粒度状态的完整可视化属于后续 UI 增强。

## UI 验收边界

本轮真实 UI 验证覆盖 Node UI 的成员选择、Agent 目录加载、选择已有 Agent、表单校验和取消操作；Manage Console 已验证静态构建和路由可用，但当前隔离环境进入控制台需要管理员凭据，因此未进行登录后的点击验收。后续应补充登录态的 Manage Console E2E。
