# node/internal/hitl

Turn 暂停（HITL）时 Client `resume` 载荷的解析。

## 职责

| 文件 | 说明 |
|------|------|
| `resume.go` | `ParseApprovalResume`、`ParseUserInformationResume`、`ApprovalPlan` |
| `resume_test.go` | 审批/追问 resume 解析单测 |

**边界**：HITL 状态机与 pending 保存在 `turn.PendingHITL` + `session.runtime`；本包只做 **resume JSON → 结构化计划**，供 `turn.ContinueAfterResume` 使用。

## 流程

```
Orchestrator 暂停 → SSE approval_required / user_information_required
Client POST resume → session.handleResume → hitl.Parse* → tool 结果写回 history
```

## 相关文档

- Turn pending：[`../turn/pending.go`](../turn/pending.go)、[`../turn/README.md`](../turn/README.md)
- API：`POST /v1/messages`（`request_type=resume`）
