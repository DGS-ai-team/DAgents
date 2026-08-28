package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

func TestOpenAIClientStreamsAndNormalizesCacheUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("request path = %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), `"source"`) || strings.Contains(string(raw), `"provenance"`) {
			t.Fatalf("internal message provenance leaked into direct provider request: %s", raw)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":8,\"total_tokens\":108,\"prompt_cache_hit_tokens\":80,\"prompt_cache_miss_tokens\":20}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{BaseURL: strings.TrimSuffix(server.URL, "/"), Model: "test-model", APIKey: "test-key"})
	var gotUsage Usage
	result, err := client.StreamChat(context.Background(), ChatRequest{SystemPrompt: "system", Messages: []Message{UserMessage("hello", "human")}}, StreamHandler{
		OnUsage: func(usage Usage) { gotUsage = usage },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "ok" {
		t.Fatalf("content = %q", result.Content)
	}
	if !gotUsage.HasPromptCacheMetrics() || gotUsage.PromptCachedTokens() != 80 || gotUsage.PromptCacheMissTokensEffective() != 20 {
		t.Fatalf("usage = %+v", gotUsage)
	}
}

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

func TestOpenAIAdapter_marshalChatRequestMessages_serializesMultimodal(t *testing.T) {
	adapter := openAIAdapter{}
	msg, err := BuildUserMessage("look", []ContentPart{{
		Type:     "image_url",
		ImageURL: &ImageURLPart{URL: "https://example.com/a.png"},
	}}, "")
	if err != nil {
		t.Fatalf("BuildUserMessage: %v", err)
	}
	payloads, ok, err := adapter.MarshalChatRequestMessages([]Message{msg})
	if err != nil || !ok {
		t.Fatalf("MarshalChatRequestMessages ok=%v err=%v", ok, err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %d", len(payloads))
	}
	if _, isArr := payloads[0]["content"].([]map[string]any); !isArr {
		t.Fatalf("content = %#v", payloads[0]["content"])
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

func TestOpenAIAdapter_prepareOutbound_keepsReasoningWithToolCalls(t *testing.T) {
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
	if out[0].ReasoningContent != "hidden" {
		t.Fatalf("expected kept reasoning, got %q", out[0].ReasoningContent)
	}
}

func TestOpenAIAdapter_prepareOutbound_stripsFinalAssistantReasoning(t *testing.T) {
	adapter := openAIAdapter{}
	out, err := adapter.PrepareOutboundMessages([]Message{{
		Role:             "assistant",
		Content:          "answer",
		ReasoningContent: "think",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ReasoningContent != "" {
		t.Fatalf("expected stripped, got %q", out[0].ReasoningContent)
	}
}

func TestOpenAIBuildRequestExtra(t *testing.T) {
	extra := BuildRequestExtraForModel("openai", "", "enabled", "high")
	if extra["thinking"] == nil {
		t.Fatal("missing thinking")
	}
	if extra["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v", extra["reasoning_effort"])
	}
	disabled := BuildRequestExtraForModel("openai", "", "disabled", "high")
	if disabled["thinking"] == nil {
		t.Fatal("missing thinking disabled")
	}
}

func TestDeepSeekBuildRequestExtra(t *testing.T) {
	extra := BuildRequestExtraForModel("deepseek", "", "enabled", "high")
	if extra["thinking"] == nil {
		t.Fatal("missing thinking extra")
	}
	if extra["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v", extra["reasoning_effort"])
	}
}
