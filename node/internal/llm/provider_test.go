package llm

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

func TestDeepSeekAdapter_toolCallbackInheritsReasoning(t *testing.T) {
	adapter := deepSeekAdapter{}
	existing := []Message{{
		Role:             "assistant",
		ReasoningContent: "cached thinking",
		ToolCalls: []ToolCall{{
			ID: "call-original", Type: "function",
			Function: ToolCallFunction{Name: "bash_run", Arguments: "{}"},
		}},
	}}
	got := adapter.NormalizeAssistantForStorage(existing, Message{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "call-callback", Type: "function",
			Function: ToolCallFunction{Name: "tool_callback", Arguments: "{}"},
		}},
	}, logx.Discard())
	if got.ReasoningContent != "cached thinking" {
		t.Fatalf("reasoning_content = %q", got.ReasoningContent)
	}
}

func TestDeepSeekAdapter_prepareOutbound_stripsFinalAssistantReasoning(t *testing.T) {
	adapter := deepSeekAdapter{}
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "answer", ReasoningContent: "think"},
	}
	out, err := adapter.PrepareOutboundMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if out[1].ReasoningContent != "" {
		t.Fatalf("expected stripped reasoning, got %q", out[1].ReasoningContent)
	}
}

func TestDeepSeekAdapter_prepareOutbound_requiresReasoningWithToolCalls(t *testing.T) {
	adapter := deepSeekAdapter{}
	_, err := adapter.PrepareOutboundMessages([]Message{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "c1", Type: "function",
			Function: ToolCallFunction{Name: "bash_run", Arguments: "{}"},
		}},
	}})
	if err == nil {
		t.Fatal("expected error for missing reasoning_content with tool_calls")
	}
}

func TestDeepSeekAdapter_prepareOutbound_keepsReasoningWithToolCalls(t *testing.T) {
	adapter := deepSeekAdapter{}
	out, err := adapter.PrepareOutboundMessages([]Message{{
		Role:             "assistant",
		ReasoningContent: "chain",
		ToolCalls: []ToolCall{{
			ID: "c1", Type: "function",
			Function: ToolCallFunction{Name: "bash_run", Arguments: "{}"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ReasoningContent != "chain" {
		t.Fatalf("reasoning_content = %q", out[0].ReasoningContent)
	}
}

func TestOpenAIAdapter_prepareOutbound_stripsAllReasoning(t *testing.T) {
	adapter := openAIAdapter{}
	out, err := adapter.PrepareOutboundMessages([]Message{{
		Role:             "assistant",
		ReasoningContent: "hidden",
		ToolCalls: []ToolCall{{
			ID: "c1", Type: "function",
			Function: ToolCallFunction{Name: "bash_run", Arguments: "{}"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ReasoningContent != "" {
		t.Fatalf("expected stripped, got %q", out[0].ReasoningContent)
	}
}

func TestDeepSeekAdapter_requestExtra(t *testing.T) {
	extra := deepSeekAdapter{}.RequestExtra()
	if extra["thinking"] == nil {
		t.Fatal("missing thinking extra")
	}
	if extra["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v", extra["reasoning_effort"])
	}
}
