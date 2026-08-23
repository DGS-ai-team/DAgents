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

func TestEffectiveSourceReadsLegacyMessage(t *testing.T) {
	m := Message{Role: "user", Name: UserNameCompression}
	if !IsMessageSource(m, MessageSourceCompression, MessageFormSummary, UserNameCompression) {
		t.Fatalf("legacy source not recognized: %#v", EffectiveMessageSource(m))
	}
	if !IsHiddenInjectedUserMessage(m) {
		t.Fatal("legacy compression message should remain hidden")
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
