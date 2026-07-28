package turn

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// publishAssistant 推送 assistant SSE。
func (o *Orchestrator) publishAssistant(sessionID, delta string) {
	o.hub.Publish(sessionID, "assistant", map[string]any{
		"content":      delta,
		"display_type": "delta",
	})
}

// publishReasoning 推送 reasoning SSE。
func (o *Orchestrator) publishReasoning(sessionID, delta string) {
	o.hub.Publish(sessionID, "reasoning", map[string]any{
		"content":      delta,
		"display_type": "reasoning",
	})
}

// publishError 推送 error SSE。
func (o *Orchestrator) publishError(sessionID, message string) {
	o.hub.Publish(sessionID, "error", map[string]any{"message": message})
}

// publishHITLRequired 推送统一 HITL SSE；Client 按 item.hitl_type 展示与 resume。
func (o *Orchestrator) publishHITLRequired(sessionID, hitlID, message string, items []map[string]any) {
	sseItems := make([]any, len(items))
	for i, item := range items {
		sseItems[i] = item
	}
	o.hub.Publish(sessionID, "hitl_required", map[string]any{
		"hitl_id":      hitlID,
		"message":      message,
		"items":        sseItems,
		"display_type": "normal_text",
	})
}

// publishToolCallPayload 推送 tool call payload SSE。
func (o *Orchestrator) publishToolCallPayload(sessionID string, payload map[string]any) {
	o.hub.Publish(sessionID, "tool_call", payload)
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
	if args := parseJSONArgs(tc.Function.Arguments); len(args) > 0 {
		payload["arguments"] = args
	}
	if raw := strings.TrimSpace(tc.Function.Arguments); raw != "" {
		payload["raw_arguments"] = raw
	}
	for k, v := range extra {
		payload[k] = v
	}
	o.hub.Publish(sessionID, "tool_result", payload)
}

// publishDone 推送 done SSE：finish_reason、turn_complete/awaiting、tool_context_metrics。
func (o *Orchestrator) publishDone(sessionID, finishReason string) {
	o.runTurnDonePhase(sessionID, finishReason)
	payload := map[string]any{"finish_reason": finishReason}
	switch finishReason {
	case "awaiting_hitl", "awaiting_user_information", "awaiting_tool_approval":
		payload["turn_complete"] = false
		payload["awaiting"] = "hitl"
	default:
		payload["turn_complete"] = true
		payload["awaiting"] = nil
	}
	if m := o.contextMetrics(sessionID); m != nil {
		payload["tool_context_metrics"] = m.snapshot()
	}
	o.logTurnContextMetrics(sessionID, finishReason)
	o.hub.Publish(sessionID, "done", payload)
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
	o.hub.Publish(sessionID, "usage", payload)
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
	o.hub.Publish(sessionID, "usage", payload)
}

// PublishSideEffectCallback Produce 时推送 callback 形态 SSE（async / external tool loop）。
func (o *Orchestrator) PublishSideEffectCallback(sessionID string, built SideEffectMessages, sideEffectSeq uint64) {
	o.publishToolCallPayload(sessionID, map[string]any{
		"assistant_content": "",
		"tool_calls": []map[string]any{{
			"id":            built.ToolCallID,
			"name":          "tool_callback",
			"raw_arguments": built.AssistantMessage.ToolCalls[0].Function.Arguments,
		}},
		"display_type":    "normal_text",
		"deferred":        true,
		"side_effect_seq": sideEffectSeq,
	})
	tc := llm.ToolCall{
		ID:   built.ToolCallID,
		Type: "function",
		Function: llm.ToolCallFunction{
			Name: built.ToolName,
		},
	}
	extra := map[string]any{
		"partial":         false,
		"display_type":    "normal_text",
		"deferred":        true,
		"side_effect_seq": sideEffectSeq,
	}
	if built.Status != "" {
		extra["async_status"] = built.Status
	}
	o.publishToolResult(sessionID, tc, built.ForClientContent, false, extra)
}

// PublishExternalSideEffectDeferred Produce 桥接态 user_message_deferred SSE。
func (o *Orchestrator) PublishExternalSideEffectDeferred(sessionID, content, userName, triggerID string, seq uint64) {
	payload := map[string]any{
		"content":         content,
		"user_name":       llm.NormalizeUserMessageName(userName),
		"deferred":        true,
		"side_effect_seq": seq,
	}
	if strings.TrimSpace(triggerID) != "" {
		payload["trigger_id"] = triggerID
	}
	o.hub.Publish(sessionID, "user_message_deferred", payload)
}

// PublishSideEffectTurnStart 被动续跑 LLM 前通知 Client。
func (o *Orchestrator) PublishSideEffectTurnStart(sessionID, source string, pending int) {
	o.hub.Publish(sessionID, "side_effect_turn_start", map[string]any{
		"source":              source,
		"side_effect_pending": pending,
		"implicit_turn":       true,
	})
}

// PublishSideEffectApplied Apply 成功后将 Produce 条目标为已入库。
func (o *Orchestrator) PublishSideEffectApplied(sessionID string, seqs []uint64) {
	if len(seqs) == 0 {
		return
	}
	out := make([]any, len(seqs))
	for i, s := range seqs {
		out[i] = s
	}
	o.hub.Publish(sessionID, "side_effect_applied", map[string]any{
		"seqs": out,
	})
}

// PublishSideEffectsCleared ClearContext/Delete 丢弃 server 缓冲时通知 Client。
func (o *Orchestrator) PublishSideEffectsCleared(sessionID string, dropped int, seqs []uint64) {
	if dropped <= 0 {
		return
	}
	out := make([]any, len(seqs))
	for i, s := range seqs {
		out[i] = s
	}
	o.hub.Publish(sessionID, "side_effects_cleared", map[string]any{
		"dropped": dropped,
		"seqs":    out,
	})
}
