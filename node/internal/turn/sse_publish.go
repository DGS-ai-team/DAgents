package turn

import (
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// publishAssistant 推送 assistant SSE。
func (o *Orchestrator) publishAssistant(sessionID, delta string) {
	o.hub.Publish(sessionID, o.agentID, "assistant", map[string]any{
		"content":      delta,
		"display_type": "delta",
	})
}

// publishReasoning 推送 reasoning SSE。
func (o *Orchestrator) publishReasoning(sessionID, delta string) {
	o.hub.Publish(sessionID, o.agentID, "reasoning", map[string]any{
		"content":      delta,
		"display_type": "reasoning",
	})
}

// publishError 推送 error SSE。
func (o *Orchestrator) publishError(sessionID, message string) {
	o.hub.Publish(sessionID, o.agentID, "error", map[string]any{"message": message})
}

// publishUserInformationRequired 推送 user information required SSE。
func (o *Orchestrator) publishUserInformationRequired(sessionID, question string, uiArgs map[string]any) {
	o.hub.Publish(sessionID, o.agentID, "user_information_required", map[string]any{
		"content":               question,
		"user_information_args": uiArgs,
		"display_type":          "normal_text",
	})
}

// publishApprovalRequired 推送 approval required SSE。
func (o *Orchestrator) publishApprovalRequired(sessionID, approvalID, executionID, message string, toolItems []map[string]any) {
	o.hub.Publish(sessionID, o.agentID, "approval_required", map[string]any{
		"approval_type": "execute_tool",
		"approval_id":   approvalID,
		"execution_id":  executionID,
		"message":       message,
		"approval_args": map[string]any{"tool_calls": toolItems},
		"display_type":  "normal_text",
	})
}

// publishToolCallPayload 推送 tool call payload SSE。
func (o *Orchestrator) publishToolCallPayload(sessionID string, payload map[string]any) {
	o.hub.Publish(sessionID, o.agentID, "tool_call", payload)
}

// publishToolCall 推送 tool call SSE。
func (o *Orchestrator) publishToolCall(sessionID string, tc llm.ToolCall, partial bool, toolIndex int) {
	if partial {
		o.logger.Debug("tool call partial",
			"session_id", sessionID,
			"tool_name", tc.Function.Name,
			"tool_index", toolIndex,
		)
	} else {
		o.logger.Info("tool call",
			"session_id", sessionID,
			"tool_name", tc.Function.Name,
			"tool_call_id", tc.ID,
		)
	}
	payload := map[string]any{
		"partial": partial,
		"tool_calls": []map[string]any{{
			"id":   tc.ID,
			"type": tc.Type,
			"function": map[string]any{
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			},
		}},
	}
	if toolIndex >= 0 {
		payload["tool_index"] = toolIndex
	}
	o.publishToolCallPayload(sessionID, payload)
}

// publishToolResult 推送 tool result SSE。
func (o *Orchestrator) publishToolResult(sessionID string, tc llm.ToolCall, content string, rejected bool, extra map[string]any) {
	payload := map[string]any{
		"tool_call_id": tc.ID,
		"tool_name":    tc.Function.Name,
		"content":      content,
		"rejected":     rejected,
	}
	for k, v := range extra {
		payload[k] = v
	}
	o.hub.Publish(sessionID, o.agentID, "tool_result", payload)
}

// publishAsyncToolCallback 推送 async tool callback SSE。
func (o *Orchestrator) publishAsyncToolCallback(sessionID string, built asyncToolMessages) {
	o.publishToolCallPayload(sessionID, map[string]any{
		"assistant_content": "",
		"tool_calls": []map[string]any{{
			"id":   built.ToolCallID,
			"name": "tool_callback",
			"arguments": map[string]any{
				"job_id": built.AssistantMessage.ToolCalls[0].Function.Arguments,
			},
			"raw_arguments": built.AssistantMessage.ToolCalls[0].Function.Arguments,
		}},
		"display_type": "normal_text",
	})
	tc := llm.ToolCall{
		ID:   built.ToolCallID,
		Type: "function",
		Function: llm.ToolCallFunction{
			Name: built.ToolName,
		},
	}
	o.publishToolResult(sessionID, tc, built.ForClientContent, false, asyncToolResultExtra(built))
}

// asyncToolResultExtra 构建 async tool result 额外字段。
func asyncToolResultExtra(built asyncToolMessages) map[string]any {
	extra := map[string]any{
		"partial":      false,
		"async_status": built.Status,
		"display_type": "normal_text",
	}
	if built.OutputCompressSavedPct > 0 {
		extra["output_compress_saved_pct"] = built.OutputCompressSavedPct
		extra["output_compress_raw_runes"] = built.OutputCompressRawRunes
		extra["output_compress_out_runes"] = built.OutputCompressOutRunes
	}
	return extra
}

// publishDone 推送 done SSE：finish_reason、turn_complete/awaiting、tool_context_metrics。
func (o *Orchestrator) publishDone(sessionID, finishReason string) {
	o.runTurnDonePhase(sessionID, finishReason)
	payload := map[string]any{"finish_reason": finishReason}
	switch finishReason {
	case "awaiting_user_information":
		payload["turn_complete"] = false
		payload["awaiting"] = "user_information"
	case "awaiting_tool_approval":
		payload["turn_complete"] = false
		payload["awaiting"] = "tool_approval"
	default:
		payload["turn_complete"] = true
		payload["awaiting"] = nil
	}
	if m := o.contextMetrics(sessionID); m != nil {
		payload["tool_context_metrics"] = m.snapshot()
	}
	o.logTurnContextMetrics(sessionID, finishReason)
	o.hub.Publish(sessionID, o.agentID, "done", payload)
}

// publishUsage 推送 usage SSE。
func (o *Orchestrator) publishUsage(sessionID string, llmStep int, u llm.Usage) {
	if o == nil {
		return
	}
	o.turnUsageMu.Lock()
	if o.turnUsage == nil {
		o.turnUsage = make(map[string]llm.Usage)
	}
	acc := o.turnUsage[sessionID]
	acc.AccumulateFrom(u)
	o.turnUsage[sessionID] = acc
	payload := llm.UsageSSEEvent(llmStep, u, acc)
	o.turnUsageMu.Unlock()
	o.hub.Publish(sessionID, o.agentID, "usage", payload)
}

// publishUsageIfAccumulated 在 turn 取消时补发已累计 usage，避免客户端 strip 丢失末次快照。
func (o *Orchestrator) publishUsageIfAccumulated(sessionID string, llmStep int) {
	if o == nil || o.hub == nil {
		return
	}
	o.turnUsageMu.Lock()
	acc, ok := o.turnUsage[sessionID]
	o.turnUsageMu.Unlock()
	if !ok {
		return
	}
	norm := acc
	norm.Normalize()
	if norm.PromptTokens <= 0 && norm.CompletionTokens <= 0 {
		return
	}
	payload := llm.UsageSSEEvent(llmStep, llm.Usage{}, acc)
	o.hub.Publish(sessionID, o.agentID, "usage", payload)
}
