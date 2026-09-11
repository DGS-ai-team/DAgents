package turn

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

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
		if msg.Role == "tool" && strings.TrimSpace(msg.ToolCallID) != "" && !llm.IsRecoveryPlaceholderToolResult(msg) {
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
		if !isCompleteToolCall(tc) {
			continue
		}
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
		if !isCompleteToolCall(tc) {
			continue
		}
		if _, ok := answered[tc.ID]; ok {
			continue
		}
		o.publishToolResult(sessionID, tc, content, false, extra)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, content))
		answered[tc.ID] = struct{}{}
	}
}

func isCompleteToolCall(call llm.ToolCall) bool {
	return llm.ValidateAssistantMessage(llm.Message{
		Role:      "assistant",
		ToolCalls: []llm.ToolCall{call},
	}) == nil
}
