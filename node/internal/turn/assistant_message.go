package turn

import (
	"context"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func assistantLifecycleID(ctx context.Context) string {
	execution, ok := ExecutionContextFromContext(ctx)
	if !ok {
		return ""
	}
	return llm.AssistantMessageIDForStep(execution.StepID)
}

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
