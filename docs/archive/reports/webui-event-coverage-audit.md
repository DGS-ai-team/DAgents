# WebUI 事件覆盖审计

更新时间：2026-08-21

## 结论

普通 Agent SSE 之前存在真实的事件丢失：Node 会通过 `/v1/streams` 发布运行时配置、记忆、技能、MCP 目录和工具集变更事件，但前端 `EventSource` 使用固定的命名事件监听列表，未注册这些事件。其中 `system_notice` 虽然已经在 `ChatView` 中写了处理分支，但由于没有注册，实际不会进入该分支。

本次收口将普通 Agent 事件集中登记在 `node/webui/frontend/src/sse/agentEvents.js`，由 `stream.js` 使用同一份列表注册；ChatView 对运行时事件增加可见提示和必要刷新。`execution` 的高频 `process_output` 仍由终端 WebSocket 展示，不触发 HTTP 轮询；生命周期边界触发 Activity 刷新。这样可以避免把进程输出帧误当成聊天事件造成刷新风暴。

## 普通 Agent SSE 覆盖矩阵

传输：`GET /v1/streams?agent_id=...`

| 事件 | Node 来源 | WebUI 行为 | 状态 |
| --- | --- | --- | --- |
| `assistant` | Turn 模型输出 | 追加助手文本 | 已覆盖 |
| `reasoning` | Turn 模型推理输出 | 追加推理文本 | 已覆盖 |
| `tool_call` / `tool_result` | Turn 工具步骤 | 更新工具调用、结果和 Tool Jobs | 已覆盖 |
| `usage` | Turn 用量 | 更新用量条和轮次用量 | 已覆盖 |
| `error` / `done` | Turn 终态 | 结束状态、收口部分输出、刷新上下文 | 已覆盖 |
| `hitl_required` | Turn 人机协同 | 结束当前等待并进入 HITL 队列 | 已覆盖 |
| `execution` | 进程执行事件 | 生命周期边界刷新 Activity；`process_output` 不轮询 | 已覆盖 |
| `temporary_agent_created/completed/cancelled` | 子 Agent 管理器 | 更新子 Agent 状态并写入系统行 | 已覆盖 |
| `context_compression_blocking/silent` | 上下文压缩协调器 | 更新压缩状态并刷新上下文 token | 已覆盖 |
| `user_message_deferred` | Side Effect 队列 | 添加延迟用户消息 | 已覆盖 |
| `side_effect_turn_start` | Side Effect 续跑 | 开始隐式 Turn | 已覆盖 |
| `side_effect_applied/cleared` | Side Effect 应用/失效 | 更新消息标记 | 已覆盖 |
| `system_notice` | 工具集变化 | 显示工具集变化提示 | 已修复遗漏 |
| `runtime/config-changed` | Agent、策略、Linux/MCP 配置 | 刷新 Agent 列表并显示立即/下一轮生效提示 | 已修复遗漏 |
| `memory/changed` | 长期记忆工具/API | 显示记忆更新及生效边界 | 已修复遗漏 |
| `skills/changed` | Skills catalog revision | 显示下一轮边界重新评估提示 | 已修复遗漏 |
| `mcp/catalog-changed` | MCP 目录刷新 | 显示 MCP 目录更新及生效边界 | 已修复遗漏 |
| `terminal.opened/updated/closed` | 终端会话注册表 | 推进 Terminal revision，刷新终端入口 | 已覆盖 |

### 关于生效边界

`runtime/config-changed`、`memory/changed`、`skills/changed` 和 `mcp/catalog-changed` 只负责把变化告知 UI；它们不改变当前 Turn 的快照。是否立即应用由 Node 的 runtime/snapshot 边界决定，前端只展示 `applied` 或 `next_turn` 等状态，避免 UI 给出错误的“当前回合已改变”提示。

## Turn 状态变化方案与本次落地

### 权威链路

前端不再通过“是否收到 reasoning/assistant/tool_call”推断 Turn 是否结束。Node 的 Turn Coordinator 负责生成 `TurnStateView`，生命周期事件持久化成功后发布 `turn_state`；`hydrate.turn_state` 使用同一份投影恢复页面。这样 SSE 实时态和刷新/断线恢复态具有同一语义：

```text
Turn Coordinator
      ├─ durable lifecycle journal
      ├─ turn_state SSE projection
      └─ hydrate.turn_state projection
              ↓
        WebUI turnState store
              ↓
      status line / stop / composer
```

`turn_state` 的身份必须使用 Agent/Session ID，而不是 Node ID；Node 侧已在 publisher 路径固定这一点。没有可用 publisher 的 focused test fixture 会安全跳过发布，不影响生命周期测试。

### 状态语义

| 权威 phase | UI 展示 | 结束本轮？ | 说明 |
| --- | --- | --- | --- |
| `queued` | 等待执行 | 否 | 已接受提交，但尚未进入模型请求 |
| `model_generating` + reasoning | 思考中 | 否 | 即使已有 reasoning 内容，仍保留“思考中”状态 |
| `model_generating` + assistant | 回复生成中 | 否 | 模型已进入可见回复通道 |
| `tool_executing` | 工具执行中 | 否 | 工具步骤是当前 Turn 的一部分 |
| `tool_waiting` | 等待工具审批 | 否 | 进入 HITL/工具审批等待 |
| `waiting_user` | 等待你的输入 | 否 | 等待用户补充信息 |
| `completed` / `failed` / `cancelled` / `interrupted` / `budget_exhausted` | 对应终态 | 是 | 只有权威终态才释放提交锁、停止按钮和临时流内容 |
| `idle` | 无活动 | 不是终态事件 | 表示当前没有活动 Turn；不能用来覆盖刚提交但尚未收到状态的短暂窗口 |

`done`、`assistant`、`reasoning` 和部分工具事件仍用于内容渲染，但不再单独决定 Turn 终态。为兼容旧 Node，收到 `done` 后仅做延迟 hydrate 对账；若随后收到权威终态，则取消该对账并使在途 hydrate 失效，避免旧快照覆盖已经到达的流式内容。

思考状态只由权威状态指示器负责展示。reasoning 流式条目不再额外渲染第二个指示器，因此模型产生思考内容时仍显示“思考中”，但不会出现两个重复的思考提示。连续增量到达时，同一状态会复用原有计时和 DOM；只有输出通道或权威 phase 改变时才切换状态。生成/思考指示器统一放在对话输入框下方的状态栏，消息流不再插入一个容易与主题 Logo 混淆的尾部气泡；`Changes` 胶囊也已移除。

### 提交、取消与序号边界

1. 用户提交成功后立即进入 `queued`，在收到第一条权威状态前保持提交锁，避免重复提交。
2. 用户取消是请求动作，不等同于终态；只有 `turn_state.phase=cancelled` 才清理临时内容并展示取消结果。
3. SSE 序号是 Node 进程内水位。Node 重启后 `sse_seq_hint` 变低或归零时，前端切换事件纪元并重置旧 `lastSeq`，避免把新进程的状态、思考和回复事件当作重复事件丢弃。
4. 事件去重水位、Turn 状态、内容缓冲和 ack 水位保持分离；hydrate 只恢复各自负责的状态，不用一个模糊的“已结束”标志覆盖全部状态。

### 相关实现

- Node：`node/internal/session/turn_state_view.go`、`runtime_lifecycle.go`、`hydrate_view.go`。
- WebUI：`node/webui/frontend/src/stores/turnState.js`、`stores/statusLines.js`、`views/ChatView.vue`。
- 回归测试：`turn_state_view_test.go`、`runtime_lifecycle_test.go`、`turnState.test.js`、`agent.test.js`。

## Workgroup SSE 覆盖矩阵

Workgroup 使用独立的 `/v1/workgroups/{id}/events` 和 POST 流式接口，不复用普通 Agent 事件列表。

| 事件 | 行为 | 状态 |
| --- | --- | --- |
| `workgroup.timeline` | 将 durable timeline payload 合并到事件列表；timeline 内部事件类型开放扩展 | 已覆盖 |
| `workgroup.realtime` + `queued` | 更新入队状态 | 已覆盖 |
| `workgroup.realtime` + `status` | 更新 thinking/tool/streaming 状态 | 已覆盖 |
| `workgroup.realtime` + `delta` | 增量更新临时助手文本 | 已覆盖 |
| `workgroup.realtime` + `assistant_final` | 更新临时助手最终文本 | 已覆盖 |
| `workgroup.realtime` + `final` | 清理临时状态 | 已覆盖 |
| 未知 realtime 类型 | 不静默丢弃，回源 Timeline、HITL 和队列状态 | 已覆盖兜底 |

Workgroup Timeline 的 `human_message`、`assign_started`、`assign_finished`、`system_notice`、`assistant_content`、`actor_final_text` 等事件均作为结构化 timeline 事件进入统一合并逻辑；视图只对展示布局做分类，不会因新增 timeline 类型而丢失事件。

## 文件传输 SSE

Linux 文件传输使用独立的 `/v1/transfers/events`，后端发布 `transfer.updated`，前端 `stores/transfers.js` 注册并更新传输状态；断线后使用 `after_seq`，初始连接使用 `live=1`，已有覆盖和单测。

## 本次结构性改进

1. 普通 Agent 命名事件集中登记，避免 transport 层和 ChatView 各自维护一份列表。
2. 每个已知事件都必须有非 `unknown` 的 UI policy，新增事件可通过单测发现遗漏。
3. 对未知 Agent 事件增加防御性可见提示；对未知 Workgroup realtime 事件走 durable resync，而不是静默忽略。
4. 将高频进程输出与低频生命周期刷新区分，保证 UI 及时性和请求成本的平衡。

## 验证清单

- [x] 前端单测：45 个测试文件、270 项通过，覆盖事件注册表、重复事件检查、状态映射和 EventSource 监听注册。
- [x] 前端单测：Workgroup realtime 已知类型和未知类型回源策略。
- [x] 前端构建：嵌入式静态资源成功生成。
- [x] Go 测试：Node、client、shared/config 全量测试通过；session/turn/api 竞态回归通过。
- [x] 真实 UI：普通对话和 bash 工具调用完成，工具结果正确进入聊天记录。
- [x] 真实 UI：Node 重启后重新建立 SSE，思考中显示、最终回复和事件序号水位均正常。
- [x] 真实 UI：取消本轮后显示“turn 已取消”、停止按钮消失，hydrate 返回 `phase=cancelled`、`terminal=true`、`end_reason=cancelled_by_user`。
- [x] 真实 UI：生成中刷新页面，hydrate 恢复“思考中”和停止按钮，完成后无残留忙碌状态。
- [x] 真实 UI：reasoning 阶段仅有一个 `aria-label="思考中"` 状态指示器，随后切换到“回复生成中”；工具回合的工具行与状态气泡不重复。
- [x] 真实 UI：生成/思考状态显示在底部状态栏，消息流无状态尾部气泡，左下角 `Changes` 胶囊已移除。
- [x] 真实 UI：从第二个设置页面触发运行时配置、记忆变化，聊天页显示对应生效边界提示；工具集缩减触发并显示 `system_notice`，随后已恢复原配置。
- [ ] 真实 UI：Workgroup 的 timeline、realtime、未知事件回源路径；本轮 Node 返回 `503`（Manage 未启动），代码测试和静态覆盖已完成，需在 Manage 可用的环境补跑。
- [ ] 真实 UI：文件传输进度和断线恢复；本轮没有活动传输任务，代码已有 `transfer.updated` SSE 单测，需在实际 Linux 传输环境补跑。

这份清单完成后，才进入下一阶段的 Agent 效果优化；效果优化阶段不应再同时承担基础 SSE 事件覆盖的补洞工作。
