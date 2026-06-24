# 002 · Web UI：工具名显示 `tool(—)` 与多项审批「批准」导致全部拒绝

| 字段 | 值 |
|------|-----|
| 组件 | Node Web UI（`node/webui/frontend`） |
| 状态 | **Fixing**（分支 `fix/webui-tool-display-and-approval`） |
| GitHub | [#35](https://github.com/DGS-ai-team/DAgents/issues/35) |
| 发现 | 2026-06-21 |

## 现象

1. **工具展示**：对话流或审批气泡中，工具标题形如 `read_file(—)` / `bash(—)`，括号内应为 path/command 等参数摘要。
2. **多项 HITL 审批**：同一批有 2+ 个 `execute_tool` 待审批时，点击某一行的「批准」，结果**全部工具被拒绝**（SSE `tool_result` 带 `rejected: true`）。

## 根因

### 1. 工具名 `—`

- `extractToolApprovals` 未复用 `normalizeToolCallItem`，无法读取 `function.name` / `function.arguments`。
- 当 `arguments` 为空对象 `{}` 时，`??` 不会回退到 `raw_arguments`，导致 `toolDisplayName` 缺少 path/command。
- `tool_result` SSE 原先不含 `arguments`/`raw_arguments`，结果气泡在缺少前置 `tool_call` 行时同样显示 `—`。

### 2. 批准变全拒

- Web UI `submitHitlOne` 仅提交 `{ approved: [callId], rejected: [] }`。
- Node `hitl.ParseApprovalResume` 要求 selection **覆盖全部** pending ID（见 `resume.go`）。
- 解析失败时 `turn.continueAfterApprovalResume` 对**每个** pending 工具发布 `rejected: true`（`pending_resume.go`），表现为「点批准却全拒」。

## 复现（Vitest）

```bash
cd node/webui/frontend && npm test -- src/stores/hitl.test.js src/utils/toolCalls.test.js
```

失败用例（修复前）：

- `extractToolApprovals` · empty `arguments` + valid `raw_arguments`
- `buildApprovalOneResume` · 多工具时 selection 须全覆盖

## 修复方向

- `extractToolApprovals` → `normalizeToolCallItem`；`resolveToolArgumentsFromData` 空 map 回退 `raw_arguments`。
- 新增 `buildApprovalSelectionResume` / `buildApprovalOneResume`（对齐 Go `BuildApprovalSelectionResume`）。
- `publishToolResult` 附带 `arguments` / `raw_arguments`。

## 相关代码

- `node/webui/frontend/src/stores/hitl.js`
- `node/webui/frontend/src/App.vue` · `submitHitlOne`
- `node/internal/turn/pending_resume.go` · 解析失败 → 全拒
- `node/internal/hitl/resume.go` · selection 全覆盖校验
