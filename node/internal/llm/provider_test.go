package llm

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

func TestDeepSeekAdapter_toolCallbackKeepsEmptyReasoning(t *testing.T) {
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
	if got.ReasoningContent != "" {
		t.Fatalf("reasoning_content = %q, want empty", got.ReasoningContent)
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

func TestDeepSeekAdapter_prepareOutbound_allowsEmptyReasoningWithToolCalls(t *testing.T) {
	adapter := deepSeekAdapter{}
	out, err := adapter.PrepareOutboundMessages([]Message{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "c1", Type: "function",
			Function: ToolCallFunction{Name: "bash_run", Arguments: "{}"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out[0].ToolCalls) != 1 {
		t.Fatalf("tool_calls = %+v", out[0].ToolCalls)
	}
	payloads, ok, err := adapter.MarshalChatRequestMessages(out)
	if err != nil || !ok {
		t.Fatalf("MarshalChatRequestMessages ok=%v err=%v", ok, err)
	}
	if _, ok := payloads[0]["reasoning_content"]; !ok {
		t.Fatal("expected reasoning_content key in API payload")
	}
}

func TestDeepSeekAdapter_marshalChatRequestMessages_keepsReasoningKeyForToolCalls(t *testing.T) {
	adapter := deepSeekAdapter{}
	payloads, ok, err := adapter.MarshalChatRequestMessages([]Message{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "call-1", Type: "function",
			Function: ToolCallFunction{Name: "bash_run", Arguments: "{}"},
		}},
	}})
	if err != nil || !ok {
		t.Fatalf("MarshalChatRequestMessages ok=%v err=%v", ok, err)
	}
	if payloads[0]["reasoning_content"] != "" {
		t.Fatalf("reasoning_content = %v", payloads[0]["reasoning_content"])
	}
}

func TestDeepSeekAdapter_marshalChatRequestMessages_omitsReasoningWithoutToolCalls(t *testing.T) {
	adapter := deepSeekAdapter{}
	payloads, ok, err := adapter.MarshalChatRequestMessages([]Message{{
		Role: "assistant", Content: "hi", ReasoningContent: "think",
	}})
	if err != nil || !ok {
		t.Fatalf("MarshalChatRequestMessages ok=%v err=%v", ok, err)
	}
	if _, exists := payloads[0]["reasoning_content"]; exists {
		t.Fatalf("unexpected reasoning_content: %+v", payloads[0])
	}
}

func TestOpenAIAdapter_marshalChatRequestMessages_usesDefaultEncoding(t *testing.T) {
	adapter := openAIAdapter{}
	payloads, ok, err := adapter.MarshalChatRequestMessages([]Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if ok || payloads != nil {
		t.Fatalf("ok=%v payloads=%v, want default encoding", ok, payloads)
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
