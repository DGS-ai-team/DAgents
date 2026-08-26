# Node + Web UI 全链路冒烟验收（归档）

> 本清单对应早期 Node + Web UI 冒烟；现行回归清单见 [`../../design/ui-e2e-regression-checklist.md`](../../design/ui-e2e-regression-checklist.md)。

本文用于验证 Agent Node 内嵌 Web UI 的最小可复现闭环，重点检查 Turn / Step 重构后的客户端可见行为。

## 验收范围

本验收覆盖：

1. Node 启动并提供 `/ui/`。
2. Web UI 建立实时 SSE 连接。
3. 通过设置页创建并启用 Mock LLM profile。
4. 为 Agent 选择该 profile，发送用户消息并收到 assistant 回复。
5. 通过 Node API 检查 Context 中 Turn / Step 的终态。
6. 通过 Timeline 检查生命周期事件顺序和终态事件。
7. 刷新浏览器后恢复 transcript 和实时连接。

本验收不把 Manage、Desktop focus service 或 browser service 作为 Node + Web UI 的前置依赖。

完整的工具、HITL、取消、连续对话和异步后台回调回归项见 [ui-e2e-regression-checklist.md](../../design/ui-e2e-regression-checklist.md)。

## Windows 前置条件

先构建内嵌静态资源：

```powershell
npm run build --prefix node/webui/frontend
go build ./node/cmd/dagents-node
```

`packaging/agent-client/config.yaml` 最小内容：

```yaml
listen:
  host: 127.0.0.1
  port: 18765
local:
  endpoint: http://127.0.0.1:18765
  node_id: ui-test-node
```

Windows 若 Go 构建需要 GCC：

```powershell
$env:CGO_ENABLED = '1'
$env:CC = 'C:\msys64\ucrt64\bin\gcc.exe'
$env:Path = 'C:\msys64\ucrt64\bin;C:\msys64\usr\bin;' + $env:Path
```

启动时使用真实目录名：

```powershell
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
```

## 手工 UI 步骤

1. 打开 `http://127.0.0.1:18765/ui/`，完成首次配置并创建 Agent。
2. 进入“设置 → 连接”，新增 profile：
   - 名称：`UI Mock`
   - 勾选“Mock 模式”
3. 将 `UI Mock` 设为默认，按提示重启 Node。
4. 打开 Agent 对话页，在模型选择按钮中选择 `UI Mock`。
5. 发送一条唯一测试消息，例如：`请回复：UI 链路测试成功`。
6. 确认：
   - 用户消息出现在消息记录；
   - assistant 返回 Mock 回显；
   - “实时连接”仍为“已连接”；
   - 发送按钮在请求结束后恢复可用状态。
7. 刷新页面，确认消息记录仍存在且实时连接重新建立。

## API 断言

从 Agent 列表取得 `agent_id` 后检查：

```powershell
$agent = (Invoke-RestMethod http://127.0.0.1:18765/v1/agents).agents[0]
$context = Invoke-RestMethod "http://127.0.0.1:18765/v1/agents/$($agent.agent_id)/context"
$timeline = Invoke-RestMethod "http://127.0.0.1:18765/v1/agents/$($agent.agent_id)/timeline?after_seq=0&limit=50"
```

Context 至少应满足：

| 字段 | 期望值 |
| --- | --- |
| `has_active_turn` | `false` |
| `turn_status` | `completed` |
| `step_status` | `completed` |
| `turn_end_reason` | `assistant_completed` |
| `step_end_reason` | `assistant_message_recorded` |
| `run_turn_phase` | `idle` |
| `pending_tool_calls_count` | `0` |

Timeline 至少应按以下顺序包含同一 Turn 的终态链：

```text
turn.started
step.started
turn.snapshot.created
model.request.started
model.usage.recorded
model.request.completed
assistant.message.recorded
step.completed
turn.completed
```

## 本次验证结果

环境：Windows，Node `127.0.0.1:18765`，内嵌 Web UI，Mock LLM。

- Web UI 单元测试：42 个测试文件、252 个测试通过。
- Web UI 构建：通过。
- 浏览器实测：创建 Agent、配置 Mock profile、切换 Agent 模型、发送消息、收到 assistant 回显、刷新恢复，全部通过。
- Context 实测：`completed / completed / idle`，无 active turn、无 pending tool call。
- Timeline 实测：完整生成 `turn.started → ... → step.completed → turn.completed` 生命周期链。
- Node 进程重启/HITL 后端回归：`TestProcessRestartRecovery -count=5` 通过 5/5，覆盖 pending HITL resume 和 unknown tool reconciliation。
- 浏览器错误日志：无新增 UI error；仅有 Desktop focus service 未启动导致的预期 `HTTP 503` warning。

## HITL 覆盖边界

本次浏览器流程使用 Echo Mock，它只返回 assistant 文本，不会主动生成工具调用，因此不能仅凭该流程证明 UI 的 `hitl_required → resume → turn.completed`。

HITL 与进程重启恢复由 Node 后端 E2E 覆盖：

```powershell
go test ./node/cmd/dagents-node -run TestProcessRestartRecovery -count=5
```

执行时请使用真实目录 `./node/cmd/dagents-node`。

后续若需要真正的浏览器级 HITL 验收，应增加一个仅用于测试的 OpenAI-compatible LLM stub，使其先返回 `ask_user_information`，再在 UI 提交 resume 后返回最终 assistant 消息；不要让 Echo Mock 承担这一职责。
