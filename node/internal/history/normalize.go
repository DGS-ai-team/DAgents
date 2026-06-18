package history

import "github.com/DGS-ai-team/DAgents/node/internal/llm"

// messageToJournalPayload 将消息转为 JSONL 行内 message 对象（assistant+tool_calls 保留 reasoning_content 键）。
func messageToJournalPayload(message llm.Message) map[string]any {
	return llm.MessageToJournalPayload(message)
}
