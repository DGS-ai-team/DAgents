package hooks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalHookContext_llmAfterCallSnakeCase(t *testing.T) {
	hc := BuildLLMAfterCallContext("s1", "a1", LLMAfterCallInput{
		AssistantContent: "token sk-secret12345678901234567890",
		FinishReason:     "stop",
	})
	raw, err := marshalHookContext(hc)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"assistant_content"`) {
		t.Fatalf("expected snake_case assistant_content, got %s", body)
	}
	if strings.Contains(body, `"AssistantContent"`) {
		t.Fatalf("unexpected PascalCase field in %s", body)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	llm, ok := m["llm_after_call"].(map[string]any)
	if !ok {
		t.Fatalf("llm_after_call = %#v", m["llm_after_call"])
	}
	if llm["assistant_content"] != "token sk-secret12345678901234567890" {
		t.Fatalf("assistant_content = %#v", llm["assistant_content"])
	}
}
