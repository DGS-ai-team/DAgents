package llm

import (
	"encoding/json"
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

func TestProviderPayloadsDoNotExposeAssistantDisplayIdentity(t *testing.T) {
	message := Message{
		Role:            "assistant",
		Content:         "answer",
		DisplayIdentity: AssistantDisplayIdentityForStep("step-provider"),
	}
	for name, encode := range map[string]func(Message) (map[string]any, error){
		"openai":   MessageToAPIPayload,
		"deepseek": MessageToDeepSeekAPIPayload,
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := encode(message)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"display_identity", "item_id", "content_item_id", "reasoning_item_id", "item_version"} {
				if _, leaked := payload[key]; leaked {
					t.Fatalf("internal identity field %q leaked in provider payload: %#v", key, payload)
				}
			}
		})
	}
}

func TestClientRequestIdentityPersistsInternallyButIsNotSentToProviders(t *testing.T) {
	message := Message{Role: "user", Content: "hello", ClientRequestID: "request-private-1"}
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ClientRequestID != message.ClientRequestID {
		t.Fatalf("durable message round trip lost request identity: %#v", restored)
	}
	journal := MessageToJournalPayload(message)
	if journal["client_request_id"] != "request-private-1" {
		t.Fatalf("journal lost request identity: %#v", journal)
	}
	for name, encode := range map[string]func(Message) (map[string]any, error){
		"openai":   MessageToAPIPayload,
		"deepseek": MessageToDeepSeekAPIPayload,
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := encode(message)
			if err != nil {
				t.Fatal(err)
			}
			if _, leaked := payload["client_request_id"]; leaked {
				t.Fatalf("internal request identity leaked to provider: %#v", payload)
			}
		})
	}
}

func TestAssistantDisplayIdentityRequiresStepID(t *testing.T) {
	if got := AssistantDisplayIdentityForStep(" "); got != nil {
		t.Fatalf("identity without a trusted Step ID = %#v, want nil", got)
	}
	identity := AssistantDisplayIdentityForStep("step-7")
	if identity == nil || identity.ContentItemID != "step-7/assistant/content" || identity.ReasoningItemID != "step-7/assistant/reasoning" || identity.ItemVersion != 1 {
		t.Fatalf("identity = %#v", identity)
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
