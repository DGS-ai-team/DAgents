package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestUnrespondedToolCallsAfterLastAssistant_ignoresEarlierAssistant(t *testing.T) {
	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "old"}}},
		{Role: "tool", ToolCallID: "old", Content: "done"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "new"}}},
	}
	calls := unrespondedToolCallsAfterLastAssistant(history)
	if len(calls) != 1 || calls[0].ID != "new" {
		t.Fatalf("calls = %+v", calls)
	}
}
