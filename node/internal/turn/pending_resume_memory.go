package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func (o *Orchestrator) continueAfterMemoryConflictResume(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	resumeValue map[string]any,
	pending *PendingHITL,
	toolLoopCount int,
) StepOutcome {
	items := pending.memoryConflictItems()
	if len(items) == 0 {
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("no pending memory_conflict items")}
	}
	resumeToolCallID := strings.TrimSpace(fmt.Sprint(resumeValue["tool_call_id"]))
	targetIdx := -1
	if resumeToolCallID != "" {
		if _, idx, ok := pending.findItem(resumeToolCallID); ok {
			if pending.Items[idx].MemoryConflict == nil {
				return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("tool_call_id %q is not memory_conflict", resumeToolCallID)}
			}
			targetIdx = idx
		} else {
			return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("unknown tool_call_id: %s", resumeToolCallID)}
		}
	} else if len(items) == 1 {
		for i, item := range pending.Items {
			if item.MemoryConflict != nil {
				targetIdx = i
				break
			}
		}
	} else {
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("tool_call_id required when multiple memory_conflict pending")}
	}
	if targetIdx < 0 {
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("missing memory_conflict tool call")}
	}

	item := pending.Items[targetIdx]
	tc := item.ToolCall
	meta := item.MemoryConflict
	decision, err := hitl.ParseMemoryConflictResume(resumeValue, tc.ID)
	if err != nil {
		msg := "rejected: " + err.Error()
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
	} else {
		content, saveErr := o.applyMemoryConflictDecision(ctx, decision, meta)
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
		return StepOutcome{LoopCount: toolLoopCount, ScheduleToolResult: true}
	}
	return StepOutcome{Pending: remaining, LoopCount: toolLoopCount}
}

func (o *Orchestrator) applyMemoryConflictDecision(ctx context.Context, decision hitl.MemoryConflictDecision, meta *MemoryConflictMeta) (string, error) {
	if meta == nil {
		return "", fmt.Errorf("missing memory conflict metadata")
	}
	switch decision {
	case hitl.MemoryConflictCancelled:
		return "[MEMORY_CONFLICT_CANCELLED] 用户取消了长期记忆更新。", nil
	case hitl.MemoryConflictKeepOld:
		return "已保留原有长期记忆，未写入新信息。", nil
	case hitl.MemoryConflictUseNew:
		content := strings.TrimSpace(meta.NewInformation)
		if err := o.persistLongTermWithRetry(ctx, func(_ string) string { return content }); err != nil {
			return "", err
		}
		return fmt.Sprintf("已用新信息替换长期记忆（%d 字符）。", len([]rune(content))), nil
	case hitl.MemoryConflictKeepBoth:
		desired := strings.TrimSpace(meta.MergedBoth)
		if desired == "" {
			desired = mergeRememberNoConflict(meta.ExistingContent, meta.NewInformation)
		}
		if err := o.persistLongTermWithRetry(ctx, func(_ string) string { return desired }); err != nil {
			return "", err
		}
		return fmt.Sprintf("已合并写入长期记忆（%d 字符）。", len([]rune(desired))), nil
	default:
		return "", fmt.Errorf("unsupported memory conflict decision")
	}
}

func (o *Orchestrator) persistLongTermWithRetry(ctx context.Context, apply func(existing string) string) error {
	if o.longTermStore == nil {
		return fmt.Errorf("long-term memory store unavailable")
	}
	for attempt := 0; attempt < rememberMaxCASRetries; attempt++ {
		snap, err := o.longTermStore.ReadLongTerm(ctx)
		if err != nil {
			return err
		}
		content := strings.TrimSpace(apply(snap.Content))
		if err := o.persistLongTermCAS(ctx, content, snap.Version); err != nil {
			if errors.Is(err, ErrLongTermVersionConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("long-term memory write conflict after retries")
}
