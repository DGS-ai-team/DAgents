package turn

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func assistantMessageFromResult(result llm.ChatResult) llm.Message {
	msg := llm.Message{Role: "assistant", Content: result.Content}
	if len(result.ToolCalls) > 0 {
		msg.ToolCalls = result.ToolCalls
		msg.ReasoningContent = result.ReasoningContent
		return msg
	}
	if strings.TrimSpace(result.ReasoningContent) != "" {
		msg.ReasoningContent = result.ReasoningContent
	}
	return msg
}

func hasPersistableAssistantPayload(msg llm.Message) bool {
	return strings.TrimSpace(msg.Content) != "" ||
		strings.TrimSpace(msg.ReasoningContent) != "" ||
		len(msg.ToolCalls) > 0
}

func (o *Orchestrator) persistCancelledStream(sessionID string, history *[]llm.Message, result llm.ChatResult) {
	assistant := assistantMessageFromResult(result)
	if !hasPersistableAssistantPayload(assistant) {
		return
	}
	o.appendHistory(sessionID, history, assistant)
	if len(assistant.ToolCalls) > 0 {
		o.appendMissingToolResponses(sessionID, history, assistant.ToolCalls, ToolStreamInterruptedMessage, map[string]any{"interrupted_by_stream_cancel": true})
	}
}

func lastAssistantIndex(messages []llm.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return i
		}
	}
	return -1
}

func toolResponsesAfterLastAssistant(messages []llm.Message) map[string]struct{} {
	lastAssistant := lastAssistantIndex(messages)
	if lastAssistant < 0 {
		return nil
	}
	answered := make(map[string]struct{})
	for i := lastAssistant + 1; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "tool" && strings.TrimSpace(msg.ToolCallID) != "" {
			answered[msg.ToolCallID] = struct{}{}
		}
	}
	return answered
}

func unrespondedToolCallsAfterLastAssistant(messages []llm.Message) []llm.ToolCall {
	lastAssistant := lastAssistantIndex(messages)
	if lastAssistant < 0 {
		return nil
	}
	assistant := messages[lastAssistant]
	if len(assistant.ToolCalls) == 0 {
		return nil
	}
	answered := toolResponsesAfterLastAssistant(messages)
	out := make([]llm.ToolCall, 0, len(assistant.ToolCalls))
	for _, tc := range assistant.ToolCalls {
		if _, ok := answered[tc.ID]; !ok {
			out = append(out, tc)
		}
	}
	return out
}

func (o *Orchestrator) insertMissingToolResponsesAfterAssistant(
	sessionID string,
	history *[]llm.Message,
	calls []llm.ToolCall,
	content string,
	extra map[string]any,
) bool {
	lastAssistantIdx := lastAssistantIndex(*history)
	if lastAssistantIdx < 0 || len(calls) == 0 {
		return false
	}
	answered := toolResponsesAfterLastAssistant(*history)
	insertAt := lastAssistantIdx + 1
	added := false
	for _, tc := range calls {
		if _, ok := answered[tc.ID]; ok {
			continue
		}
		o.publishToolResult(sessionID, tc, content, false, extra)
		o.insertHistory(sessionID, history, insertAt, llm.ToolResultMessage(tc.ID, tc.Function.Name, content))
		insertAt++
		answered[tc.ID] = struct{}{}
		added = true
	}
	return added
}

// RepairUnrespondedToolCalls 为尾部 assistant 尚未配对的 tool_call 补写 tool 结果，避免 LLM 400。
func (o *Orchestrator) RepairUnrespondedToolCalls(sessionID string, history *[]llm.Message) bool {
	calls := unrespondedToolCallsAfterLastAssistant(*history)
	if len(calls) == 0 {
		return false
	}
	return o.insertMissingToolResponsesAfterAssistant(
		sessionID,
		history,
		calls,
		ToolUserInterruptedMessage,
		map[string]any{"repaired_orphan_tool_calls": true},
	)
}

func (o *Orchestrator) appendMissingToolResponses(
	sessionID string,
	history *[]llm.Message,
	calls []llm.ToolCall,
	content string,
	extra map[string]any,
) {
	if len(calls) == 0 {
		return
	}
	answered := toolResponsesAfterLastAssistant(*history)
	for _, tc := range calls {
		if _, ok := answered[tc.ID]; ok {
			continue
		}
		o.publishToolResult(sessionID, tc, content, false, extra)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, content))
		answered[tc.ID] = struct{}{}
	}
}
