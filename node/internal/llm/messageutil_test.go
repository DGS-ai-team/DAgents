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

func TestEstimateMessageTokensIncludesModelToolResultMetadata(t *testing.T) {
	message := ToolResultMessage("call-1", "read_file", "body")
	prepared := PrepareToolResultMessagesForModel([]Message{message})
	want := EstimateTextTokens(prepared[0].Content) + 16
	if got := EstimateMessageTokens([]Message{message}); got != want {
		t.Fatalf("tokens = %d want %d (prepared=%q)", got, want, prepared[0].Content)
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

func TestMessageToAPIPayloadDoesNotExposeInternalToolMetadataField(t *testing.T) {
	message := ToolResultMessage("call-1", "read_file", "body")
	payload, err := MessageToAPIPayload(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["tool_result_metadata"]; ok {
		t.Fatalf("internal metadata leaked into provider payload: %+v", payload)
	}
	if payload["content"] != "body" {
		t.Fatalf("provider payload should retain raw content: %+v", payload)
	}
	if _, ok := payload["source"]; ok {
		t.Fatalf("internal source leaked into provider payload: %+v", payload)
	}
	if _, ok := payload["provenance"]; ok {
		t.Fatalf("internal provenance leaked into provider payload: %+v", payload)
	}
}

func TestMessageToJournalPayloadPersistsToolResultMetadata(t *testing.T) {
	message := ToolResultMessage("call-1", "bash_run", "ERROR: exit 1")
	payload := MessageToJournalPayload(message)
	if _, ok := payload["tool_result_metadata"]; !ok {
		t.Fatalf("journal payload lost tool metadata: %+v", payload)
	}
}

func TestMessageToJournalPayloadPersistsSourceAndProvenance(t *testing.T) {
	message := UserMessage("skill body", UserNameSkill)
	payload := MessageToJournalPayload(message)
	if _, ok := payload["source"]; !ok {
		t.Fatalf("journal payload lost source: %+v", payload)
	}
	if _, ok := payload["provenance"]; !ok {
		t.Fatalf("journal payload lost provenance: %+v", payload)
	}
}
