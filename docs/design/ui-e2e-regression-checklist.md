# Web UI 回归测试清单

本文是 Node 内嵌 Web UI 的可重复回归清单，覆盖真实模型、工具调用、HITL、Turn 取消、连续对话和异步后台回调。测试目标是验证“浏览器 → SSE → Node MessageQueue → Turn/Step → 工具/后台 job → 回灌 → UI”的完整链路。

## 1. 测试前置

| 项目 | 要求 |
| --- | --- |
| Node | `127.0.0.1:18765` 可访问，`/ui/` 返回 200 |
| Web UI | 内嵌页面可打开，实时连接显示“已连接” |
| Agent | 已创建可测试 Agent |
| LLM | 推荐使用真实模型；Mock 仅用于页面基础冒烟 |
| 工作区 | 使用测试工作区，避免在生产目录执行命令 |
| Manage / Desktop / browser service | 本清单不要求启动；未启动时对应 503 warning 不视为 Node + UI 失败 |

真实模型测试前，在“设置 → 连接”确认：

- 当前模型 profile 显示“密钥已配置”；
- 对话页模型按钮显示真实 profile，而不是 Mock；
- 不在测试输出、截图或报告中记录 API Key。

## 2. 基础连接与会话恢复

| 编号 | 操作 | 预期 |
| --- | --- | --- |
| B-01 | 打开 `/ui/` | 页面正常加载，Agent 列表可见 |
| B-02 | 进入 Agent 对话 | SSE 显示“已连接” |
| B-03 | 发送短消息 | 用户消息和 assistant 回复均出现，发送按钮恢复 |
| B-04 | 刷新页面 | transcript 恢复，SSE 重新连接，不能重复生成回复 |
| B-05 | 查询 Context | `has_active_turn=false`、无 pending tool call、Turn/Step 为终态 |

## 3. 真实模型基础调用

发送：

```text
请只回复：真实模型测试成功
```

断言：

- 回复来自当前真实模型，而非 Mock 回显；
- UI 显示真实 token / reasoning 用量；
- Context 的 `turn_end_reason=assistant_completed`；
- Timeline 至少包含 `turn.started → step.started → model.request.started → model.request.completed → assistant.message.recorded → step.completed → turn.completed`。

## 4. 文件工具调用

### T-01：只读工具成功

发送：

```text
请调用 glob_files 工具，列出当前工作区根目录下最多 5 个文件：glob_pattern 使用 **/*，directory 使用 .，max_results 使用 5。只读，不要调用其他工具；工具返回后简要告诉我结果。
```

预期：

- UI 出现 fs 工具卡片和成功结果；
- 工具结果进入后续模型 Step；
- Timeline 出现 `tool.batch.created → tool.call.recorded → tool.execution.started → tool.execution.completed → tool.result.recorded → tool.batch.settled`；
- Turn 最终完成，无 pending tool call。

### T-02：只读工具失败

发送：

```text
请调用 read_file 工具读取相对路径 __ui_e2e_missing_file__，line_offset 使用 0，line_limit 使用 20。这个文件故意不存在；工具返回错误后，请明确说明读取失败并结束，不要调用其他工具，也不要创建或修改文件。
```

预期：

- UI 展示工具失败结果，而不是页面崩溃；
- 模型能基于失败结果回复；
- `tool.execution.failed` 和 `tool.result.recorded` 可在生命周期/调试数据中观察到；
- Turn 正常结束，不自动执行写入或危险重试。

## 5. bash 与审批

本组测试前，在 Agent 能力中临时启用“命令行”；结束后关闭，恢复原工具组。

### T-03：bash 只读调用

发送：

```text
请调用 bash_run 工具，在当前工作区执行只读命令 pwd，然后告诉我命令输出。不要调用其他工具；不要修改、创建或删除任何文件。
```

预期：

- UI 出现 shell 工具卡片；
- 输出为当前测试工作目录；
- Turn/Step 正常完成。

### T-04：bash HITL 审批

将 `bash_run` 工具策略临时改为“需审批”，发送同类 `pwd` 请求。

预期：

1. UI 出现“待审批”和“批准 / 拒绝”；
2. 点击“批准”；
3. 命令只执行一次；
4. 工具结果回到模型，模型继续回复；
5. Timeline 出现 `interaction.requested → interaction.resolved → tool.execution.started → tool.execution.completed`；
6. 无 pending interaction。

测试后将 `bash_run` 策略恢复为“特殊规则”。

## 6. Agent 询问 HITL

本组测试前临时启用“用户询问”能力，结束后关闭。

发送：

```text
请调用 ask_user_information 工具询问我一个问题：我最喜欢的颜色是什么？只询问一次并等待我的回答，收到回答后回复确认。
```

在 UI 输入一个测试答案并提交。

预期：

- UI 展示 Agent 询问；
- 输入答案后产生一次 resume；
- 模型能够读取答案并继续回复；
- Timeline 出现 `interaction.requested → interaction.resolved`；
- Turn 最终完成，无 pending HITL。

## 7. Turn 取消与连续对话

### T-05：取消长响应

发送一条明确要求长篇生成、且不调用工具的消息，模型开始输出后点击“停止本轮”。

预期：

- UI 显示“turn 已取消”；
- Context：
  - `turn_status=cancelled`；
  - `step_status=cancelled`；
  - `turn_end_reason=cancelled_by_user`；
  - `step_end_reason=cancelled_by_user`；
  - `has_active_turn=false`；
  - `pending_tool_calls_count=0`；
- Timeline 顺序包含 `step.cancelled → turn.cancelled`；
- 输入框、发送按钮和 SSE 状态恢复正常。

### T-06：取消后的连续对话

连续发送两条无工具消息：

```text
请记住短语“连续对话校验”，然后只回复：已记住。不要调用工具。
```

```text
刚才让你记住的短语是什么？只回复这个短语，不要调用工具。
```

预期：第二条正确回复“连续对话校验”，且两轮都产生独立完整的 `turn.completed`。

## 8. 异步后台回调（消息队列重点）

本组测试前临时启用“命令行”，确保 `bash_run` 策略允许安全测试命令；结束后关闭。

发送：

```text
请调用 bash_run 工具执行一次异步回调测试：command 使用 PowerShell 语句 Start-Sleep -Seconds 3; Write-Output async-callback-ok，timeout_seconds 使用 1，shell_type 使用 powershell。工具如果返回后台 job 或 RUNNING，不要调用 background_job_status 轮询，等待系统自动回调 async_tool_result；收到回调结果后只回复 callback-ok。不要调用其他工具。
```

若 UI 将该命令判定为高风险，批准本次无副作用测试命令。

预期：

1. 第一次工具卡片显示“已提交后台任务，等待系统自动回调结果…”；
2. 后台 job 完成后出现“已入库”；
3. UI 展示 `callback-ok`；
4. 模型收到异步结果并完成后续回复；
5. 不需要 `background_job_status` 轮询；
6. `GET /v1/agents/{agent_id}/tool-jobs` 最终显示 `running=0`、`background=0`；
7. Context 最终为无 active turn、无 pending tool call、Turn/Step completed；
8. 不出现重复 callback 或重复 assistant 回复。

该测试对应的 Node 内部链路是：

```text
bash_run 超时
  → background job
  → async_tool_result
  → MessageQueue async_completion
  → side-effect Produce / SSE
  → Apply / continuation
  → 后续模型 Step
  → turn.completed
```

### T-07：异步回调与用户询问并行挂起（已修复）

用于验证消息队列在同一工具批次同时包含 `ask_user_information` 与超时降级的 `bash_run` 时，是否能分别处理两个待处理项。

测试步骤：

1. 同一条用户消息要求模型同时调用 `ask_user_information` 和 `bash_run`；
2. 在 Turn 仍处于执行状态时，通过输入框回答用户问题，验证 Enter/发送可以提交 HITL resume；
3. 批准 `bash_run`，等待后台 job 自动回调；
4. 等待模型完成，并检查生命周期、队列和后台任务状态。

复测结果（2026-08-21，真实模型 `mimo-v2.5-pro`，重启到本次重构后的 Node 二进制）：

- UI 在 `sending=true` 且队首为 `user_information` 时允许输入并提交回答；普通执行中的草稿发送门控仍保持不变；
- 部分 resume 后，生命周期投影会保存“剩余 HITL”而不是从原始 assistant 工具批次恢复完整队列；
- 异步结果在 HITL 未清空前保持旁路缓冲，清空后只应用一次；callback history 只作为模型上下文，不再注册为新的 ToolExecution；
- Timeline 出现 `external.fact.recorded`，`external_fact_kind=async`，绑定原始 bash `tool_call_id`，未生成 callback ToolExecution；
- 重启后的真实混合场景最终回复为 `fresh-external-fact-complete`，`has_active_turn=false`、无 pending HITL、无后台任务；
- 旧进程上的先前混合场景仍可恢复，最终回复为 `mixed-refactor-complete`；未出现重复 callback 或 `cannot start from succeeded`。

已落地的修复点：

1. `MainChatPanel` 对 `user_information` 队首提供独立的发送门控；
2. `lifecycleAfterResume` 持久化部分 resume 后的剩余 HITL payload；
3. 异步 side effect 在 HITL 清空前不修改历史；生命周期只接受当前 ToolBatch 已登记的工具调用，并将 Apply 记录为 external fact；
4. 增加对应的前端单元测试、生命周期回归测试和本清单中的真实 UI 混合场景验证。

## 9. 每轮通用 API 检查

```powershell
$agent = (Invoke-RestMethod http://127.0.0.1:18765/v1/agents).agents[0]
$context = Invoke-RestMethod "http://127.0.0.1:18765/v1/agents/$($agent.agent_id)/context"
$jobs = Invoke-RestMethod "http://127.0.0.1:18765/v1/agents/$($agent.agent_id)/tool-jobs"
$timeline = Invoke-RestMethod "http://127.0.0.1:18765/v1/agents/$($agent.agent_id)/timeline?after_seq=0&limit=200"
```

重点检查：

| 字段 | 正常完成 | 取消场景 |
| --- | --- | --- |
| `has_active_turn` | `false` | `false` |
| `turn_status` | `completed` | `cancelled` |
| `step_status` | `completed` | `cancelled` |
| `run_turn_phase` | `idle` | `idle` |
| `pending_tool_calls_count` | `0` | `0` |
| 后台 job running 数 | `0` | `0` |

## 10. 测试收尾

- 关闭本轮临时启用的命令行、用户询问等工具组；
- 恢复工具策略（例如 `bash_run=特殊规则`）；
- 确认没有 pending HITL、running job 或未消费队列项；
- 刷新一次 UI，确认 transcript 和 SSE 仍正常；
- 不删除测试 Agent 或历史记录，便于复盘；
- 不在报告中记录 API Key、密钥内容或敏感文件内容。

## 11. 本次基线结果（Windows，真实模型）

本次使用真实模型 `mimo-v2.5-pro` 完成：

- 真实模型基础对话：通过；
- `glob_files` 成功调用：通过；
- `read_file` 故意失败处理：通过；
- `bash_run pwd`：通过；
- bash 审批 → 执行 → 回复：通过；
- `ask_user_information` → resume → 回复：通过；
- Turn 取消：通过；
- 取消后的连续对话与上下文回忆：通过；
- 连续对话第一轮/第二轮 `continuity-one` → `continuity-two`：通过；
- 重启本次重构后的 Node 后重新执行混合 HITL + async：通过，并观测到 `external.fact.recorded`；
- bash 超时降级后台 job → `async_tool_result` 自动回灌 → 后续回复：通过；
- 普通异步回调（单独运行）：通过；
- 异步回调与用户询问并行挂起：通过，见 T-07；
- 最终工具组恢复为 `fs + skills`；
- 测试收尾后无 active turn、无 pending HITL、无 pending tool call，后台 job 数为 0；混合场景 Turn 终态为 completed；
- 浏览器无新增 error，仅有未启动 Desktop focus service 的预期 503 warning。

## 12. 当前未覆盖项

以下功能需要额外环境或配置，不纳入本次基线：

- Linux SSH 通道与远程命令异步回调；
- MCP 服务工具；
- 子 Agent 与父 Agent 消息回传；
- 浏览器服务工具；
- Trigger / Workgroup / A2A 外部消息回调。
