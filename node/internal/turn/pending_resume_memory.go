package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
		return StepOutcome{StepIndex: stepIndex, ScheduleToolResult: true}
	}
	return StepOutcome{Pending: remaining, StepIndex: stepIndex}
}

func (o *Orchestrator) applyMemoryConflictDecision(ctx context.Context, decision hitl.MemoryConflictDecision, meta *MemoryConflictMeta) (string, error) {
	if meta == nil {
		return "", fmt.Errorf("missing memory conflict metadata")
	}
	if meta.ConflictID != "" && o.memoryService != nil {
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
			// must keep its frozen MemorySnapshot. Consumers apply this event on
			// the next Turn boundary; it is never routed through InputBox.
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
		}
	}
	switch decision {
	case hitl.MemoryConflictCancelled:
		return "[MEMORY_CONFLICT_CANCELLED] 用户取消了长期记忆更新。", nil
	case hitl.MemoryConflictKeepOld:
		return "已保留原有长期记忆，未写入新信息。", nil
	case hitl.MemoryConflictUseNew:
		content := strings.TrimSpace(meta.NewInformation)
		entries := []LongTermEntry{NewLongTermEntry(content, time.Now().UTC())}
		if err := o.persistLongTermWithRetry(ctx, func(_ []LongTermEntry) []LongTermEntry { return entries }); err != nil {
			return "", err
		}
		return fmt.Sprintf("已用新信息替换长期记忆（%d 条）。", len(entries)), nil
	case hitl.MemoryConflictKeepBoth:
		desired := strings.TrimSpace(meta.MergedBoth)
		if desired == "" {
			desired = meta.ExistingContent + "\n\n" + meta.NewInformation
		}
		entries := EntriesFromFormattedConflict(desired)
		if err := o.persistLongTermWithRetry(ctx, func(_ []LongTermEntry) []LongTermEntry { return entries }); err != nil {
			return "", err
		}
		return fmt.Sprintf("已合并写入长期记忆（%d 条）。", countNonEmptyEntries(entries)), nil
	default:
		return "", fmt.Errorf("unsupported memory conflict decision")
	}
}

func (o *Orchestrator) persistLongTermWithRetry(ctx context.Context, apply func(existing []LongTermEntry) []LongTermEntry) error {
	if o.longTermStore == nil {
		return fmt.Errorf("long-term memory store unavailable")
	}
	for attempt := 0; attempt < rememberMaxCASRetries; attempt++ {
		snap, err := o.longTermStore.ReadLongTerm(ctx)
		if err != nil {
			return err
		}
		entries := apply(snap.Entries)
		if err := o.persistLongTermCAS(ctx, entries, snap.Version); err != nil {
			if errors.Is(err, ErrLongTermVersionConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("long-term memory write conflict after retries")
}
