package history

import (
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func cloneMessage(message llm.Message) llm.Message {
	raw, err := json.Marshal(message)
	if err != nil {
		return message
	}
	var out llm.Message
	if err := json.Unmarshal(raw, &out); err != nil {
		return message
	}
	return out
}

// messageToJournalPayload 将消息转为 JSONL 行内 message 对象（assistant+tool_calls 保留 reasoning_content 键）。
func messageToJournalPayload(message llm.Message) map[string]any {
	raw, err := json.Marshal(message)
	if err != nil {
		return map[string]any{"role": message.Role, "content": message.Content}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{"role": message.Role, "content": message.Content}
	}
	if message.Role == "assistant" && len(message.ToolCalls) > 0 {
		payload["reasoning_content"] = message.ReasoningContent
	}
	return payload
}
