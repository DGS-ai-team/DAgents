package turn

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func (o *Orchestrator) continueAfterUserInformationResume(
	_ context.Context,
	sessionID string,
	history *[]llm.Message,
	resumeValue map[string]any,
	pending *PendingHITL,
	toolLoopCount int,
) StepOutcome {
	pending.Normalize()
	resumeToolCallID := strings.TrimSpace(fmt.Sprint(resumeValue["tool_call_id"]))
	var targetIdx = -1
	if resumeToolCallID != "" {
		if _, idx, ok := pending.findItem(resumeToolCallID); ok {
			if !tools.IsAskUserInformation(pending.Items[idx].ToolCall.Function.Name) {
				return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("tool_call_id %q is not user_information", resumeToolCallID)}
			}
			targetIdx = idx
		} else {
			return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("unknown tool_call_id: %s", resumeToolCallID)}
		}
	} else {
		userItems := pending.userInformationItems()
		if len(userItems) != 1 {
			return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("tool_call_id required when multiple user_information pending")}
		}
		for i, item := range pending.Items {
			if item.ToolCall.ID == userItems[0].ToolCall.ID {
				targetIdx = i
				break
			}
		}
	}
	if targetIdx < 0 {
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("missing user_information tool call")}
	}

	tc := pending.Items[targetIdx].ToolCall
	content, err := hitl.ParseUserInformationResume(resumeValue, tc.ID)
	if err != nil {
		content = err.Error()
	}
	o.publishToolResult(sessionID, tc, content, false, nil)
	o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, content))

	remaining := pending.withoutIndex(targetIdx)
	if remaining == nil {
		return StepOutcome{LoopCount: toolLoopCount, ScheduleToolResult: true}
	}
	return StepOutcome{Pending: remaining, LoopCount: toolLoopCount}
}

func (o *Orchestrator) continueAfterApprovalResume(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	resumeValue map[string]any,
	pending *PendingHITL,
	toolLoopCount int,
) StepOutcome {
	pending.Normalize()
	approvalItems := pending.approvalItems()
	if len(approvalItems) == 0 {
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("no pending approval tool calls")}
	}

	ids := make([]string, 0, len(approvalItems))
	for _, item := range approvalItems {
		ids = append(ids, item.ToolCall.ID)
	}

	plan, err := hitl.ParseApprovalResume(resumeValue, ids)
	if err != nil {
		for _, item := range approvalItems {
			tc := item.ToolCall
			msg := "rejected: " + err.Error()
			o.publishToolResult(sessionID, tc, msg, true, nil)
			o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		}
	} else {
		var approved []llm.ToolCall
		for _, item := range approvalItems {
			tc := item.ToolCall
			if plan.IsApproved(tc.ID) {
				approved = append(approved, tc)
			} else {
				msg := "rejected: user_rejected"
				o.publishToolResult(sessionID, tc, msg, true, nil)
				o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
			}
		}
		if err := o.executeAutoBatch(ctx, sessionID, history, approved, &plan); err != nil {
			return StepOutcome{LoopCount: toolLoopCount, Err: err}
		}
	}

	remainingItems := pending.userInformationItems()
	if len(remainingItems) == 0 {
		return StepOutcome{LoopCount: toolLoopCount, ScheduleToolResult: true}
	}
	return StepOutcome{Pending: pendingFromItems(remainingItems), LoopCount: toolLoopCount}
}
