package turn

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// assistantMessageFromResult converts a completed model response to the
// canonical assistant history message. It must not be used for a partial
// response returned with context.Canceled.
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
