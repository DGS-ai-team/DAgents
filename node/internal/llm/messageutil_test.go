package llm

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

func TestEstimateMessageTokens_includesReasoning(t *testing.T) {
	got := EstimateMessageTokens([]Message{{
		Role:             "assistant",
		Content:          "abcd",
		ReasoningContent: "wxyz",
	}})
	want := tokens.EstimateInt("abcd") + 16 + tokens.EstimateInt("wxyz")
	if got != want {
		t.Fatalf("tokens = %d want %d", got, want)
	}
}

func TestMessageToDeepSeekAPIPayload_keepsReasoningKeyForToolCalls(t *testing.T) {
	payload, err := MessageToDeepSeekAPIPayload(Message{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "call-1", Type: "function",
			Function: ToolCallFunction{Name: "bash_run", Arguments: "{}"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["reasoning_content"]; !ok {
		t.Fatalf("missing reasoning_content: %+v", payload)
	}
}

func TestMessageToJournalPayload_keepsReasoningKeyForToolCalls(t *testing.T) {
	payload := MessageToJournalPayload(Message{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "call-1", Type: "function",
			Function: ToolCallFunction{Name: "bash_run", Arguments: "{}"},
		}},
	})
	if _, ok := payload["reasoning_content"]; !ok {
		t.Fatalf("missing reasoning_content: %+v", payload)
	}
}
