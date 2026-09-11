package turn

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
)

func (o *Orchestrator) continueAfterMemoryConflictResume(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	resumeValue map[string]any,
	pending *PendingHITL,
	stepIndex int,
) StepOutcome {
	items := pending.memoryConflictItems()
	if len(items) == 0 {
		return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("no pending memory_conflict items")}
	}
	resumeToolCallID := strings.TrimSpace(fmt.Sprint(resumeValue["tool_call_id"]))
	targetIdx := -1
	if resumeToolCallID != "" {
		if _, idx, ok := pending.findItem(resumeToolCallID); ok {
			if pending.Items[idx].MemoryConflict == nil {
				return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("tool_call_id %q is not memory_conflict", resumeToolCallID)}
			}
			targetIdx = idx
		} else {
			return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("unknown tool_call_id: %s", resumeToolCallID)}
		}
	} else if len(items) == 1 {
		for i, item := range pending.Items {
			if item.MemoryConflict != nil {
				targetIdx = i
				break
			}
		}
	} else {
		return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("tool_call_id required when multiple memory_conflict pending")}
	}
	if targetIdx < 0 {
		return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("missing memory_conflict tool call")}
	}

	item := pending.Items[targetIdx]
	tc := item.ToolCall
	decision, err := hitl.ParseMemoryConflictResume(resumeValue, tc.ID)
	if err != nil {
		msg := "rejected: " + err.Error()
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
	} else {
		content, saveErr := o.applyMemoryConflictDecision(ctx, decision, item.MemoryConflict)
		if saveErr != nil {
			msg := "ERROR: " + saveErr.Error()
			o.publishToolResult(sessionID, tc, msg, true, nil)
			o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		} else {
			o.publishToolResult(sessionID, tc, content, false, nil)
			o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, content))
		}
	}

	remaining := pending.withoutIndex(targetIdx)
	if remaining == nil {
		return StepOutcome{StepIndex: stepIndex, ScheduleToolResult: true}
	}
	return StepOutcome{Pending: remaining, StepIndex: stepIndex}
}

func (o *Orchestrator) applyMemoryConflictDecision(ctx context.Context, decision hitl.MemoryConflictDecision, meta *MemoryConflictMeta) (string, error) {
	if meta == nil || o == nil || o.memoryService == nil {
		return "", fmt.Errorf("memory service unavailable")
	}
	mapped := memory.ConflictCancel
	switch decision {
	case hitl.MemoryConflictKeepOld:
		mapped = memory.ConflictKeepOld
	case hitl.MemoryConflictUseNew:
		mapped = memory.ConflictUseNew
	case hitl.MemoryConflictKeepBoth:
		mapped = memory.ConflictKeepBoth
	case hitl.MemoryConflictCancelled:
		mapped = memory.ConflictCancel
	default:
		return "", fmt.Errorf("unsupported memory conflict decision")
	}
	result, err := o.memoryService.ResolveConflict(ctx, memory.Scope(meta.Scope), meta.ConflictID, mapped)
	if err != nil {
		return "", err
	}
	if o.hub != nil {
		// Resolving the conflict mutates durable memory, but the active Turn
		// keeps its frozen MemorySnapshot until the next Turn boundary.
		o.hub.Publish(o.agentID, "memory/changed", map[string]any{
			"agent_id": o.agentID, "store_revision": result.StoreRevision,
			"outcome": string(result.Outcome), "turn_boundary": "next_turn",
		})
	}
	switch decision {
	case hitl.MemoryConflictCancelled:
		return "[MEMORY_CONFLICT_CANCELLED] 用户取消了长期记忆更新。", nil
	case hitl.MemoryConflictKeepOld:
		return "已保留原有长期记忆，未写入新信息。", nil
	case hitl.MemoryConflictUseNew:
		return fmt.Sprintf("已用新信息替换长期记忆（%d 条）。", len(result.Superseded)), nil
	case hitl.MemoryConflictKeepBoth:
		return "已保留冲突双方，标记为待确认记忆。", nil
	default:
		return "", fmt.Errorf("unsupported memory conflict decision")
	}
}
