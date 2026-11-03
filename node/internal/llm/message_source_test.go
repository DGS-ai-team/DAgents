package llm

import (
	"encoding/json"
	"testing"
)

func TestUserMessageMaterializesStructuredSource(t *testing.T) {
	m := UserMessage("write it", UserNameSkill)
	if m.Source == nil || m.Source.Kind != MessageSourcePlugin || m.Source.Form != MessageFormInstructions {
		t.Fatalf("source = %#v", m.Source)
	}
	if m.Provenance == nil || m.Provenance.Producer != "skills" {
		t.Fatalf("provenance = %#v", m.Provenance)
	}
}

func TestMessageSourceSurvivesJSONRoundTrip(t *testing.T) {
	original := UserMessageWithSource(
		"body",
		UserNameSkill,
		MessageSource{Kind: MessageSourcePlugin, Form: MessageFormInstructions},
		&MessageProvenance{Producer: "skills", Operation: "load", Reference: "writer"},
	)
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Source == nil || restored.Source.Kind != MessageSourcePlugin || restored.Provenance == nil || restored.Provenance.Reference != "writer" {
		t.Fatalf("restored = %#v", restored)
	}
}

func TestPartialAssistantMessageRemainsPlainTextForNextModelTurn(t *testing.T) {
	partial := Message{
		Role:    "assistant",
		Content: "partial answer",
		Source:  &MessageSource{Kind: MessageSourceModel},
		Provenance: &MessageProvenance{
			Producer: "model", Operation: "partial_response", Reference: "turn-1/step-1",
		},
	}
	if !IsPartialAssistantMessage(partial) {
		t.Fatal("partial assistant marker was not recognized")
	}
	payloads, err := MessagesToAPIPayload([]Message{{Role: "user", Content: "continue"}, partial})
	if err != nil {
		t.Fatal(err)
	}
	if payloads[1]["role"] != "assistant" || payloads[1]["content"] != "partial answer" {
		t.Fatalf("provider partial payload = %#v", payloads[1])
	}
	if _, ok := payloads[1]["source"]; ok {
		t.Fatalf("internal source leaked to provider: %#v", payloads[1])
	}
}

func TestConversationBoundaryIsOnlyHiddenForIdleContextProducer(t *testing.T) {
	m := UserMessage("private bridge", UserNameConversationBoundary)
	if m.Source == nil || m.Source.Kind != MessageSourceRuntime || m.Source.Form != MessageFormNotice || m.Provenance == nil || m.Provenance.Producer != "idle_context" {
		t.Fatalf("boundary metadata = %#v / %#v", m.Source, m.Provenance)
	}
	if !IsHiddenInjectedUserMessage(m) {
		t.Fatal("idle context bridge should be hidden")
	}
	other := UserMessageWithSource("runtime content", "runtime", MessageSource{Kind: MessageSourceRuntime, Form: MessageFormNotice}, &MessageProvenance{Producer: "other_runtime"})
	if IsHiddenInjectedUserMessage(other) {
		t.Fatal("other runtime user content must remain visible")
	}
}
