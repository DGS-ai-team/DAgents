package compression

import (
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func snapshotMessages(messages []llm.Message) []llm.Message {
	return append([]llm.Message(nil), messages...)
}

func messagesFingerprint(messages []llm.Message) string {
	raw, err := json.Marshal(messages)
	if err != nil {
		return ""
	}
	return string(raw)
}
