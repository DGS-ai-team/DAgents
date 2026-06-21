# node/internal/hitl

**resume JSON 解析**（`ParseApprovalResume`、`ParseUserInformationResume`、`ResumeValueKind`）。供 `turn.ContinueAfterResume` 使用。

**边界**：HITL 状态机与 pending 保存在 `turn.PendingHITL` + `session.runtime`；本包只做 **resume JSON → 结构化计划**，不推送 SSE。

## 数据流

```text
Orchestrator 暂停 → SSE hitl_required（本地 turn）
                 或 approval_required / user_information_required（A2A 中继）
Client POST resume → session.handleResume → hitl.Parse* → tool 结果写回 history
```

## 文件

| 文件 | 说明 |
|------|------|
| `resume.go` | `ParseApprovalResume`、`ParseUserInformationResume`、`ApprovalPlan` |
| `trigger_session.go` | trigger 审批投递目标解析 |
| `resume_test.go` | 解析单测 |
