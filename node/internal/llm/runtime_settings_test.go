package llm

import "testing"

func TestBuildRequestExtra_deepseek(t *testing.T) {
	extra := BuildRequestExtra("deepseek", "enabled", "max")
	if extra["reasoning_effort"] != "max" {
		t.Fatalf("effort = %v", extra["reasoning_effort"])
	}
	disabled := BuildRequestExtra("deepseek", "disabled", "max")
	if disabled["reasoning_effort"] != nil {
		t.Fatalf("disabled should not set effort: %v", disabled)
	}
	if Thinking := disabled["thinking"]; Thinking == nil {
		t.Fatal("missing thinking")
	}
}

func TestRuntimeSettingsApplyPatch(t *testing.T) {
	s := &RuntimeSettings{Provider: "deepseek", Model: "deepseek-chat", Thinking: "enabled", ReasoningEffort: "high"}
	off := "disabled"
	view, err := s.ApplyPatch(LLMSettingsPatch{Thinking: &off})
	if err != nil {
		t.Fatal(err)
	}
	if view.Thinking != "disabled" || view.ReasoningEffort != "" {
		t.Fatalf("view = %+v", view)
	}
	max := "max"
	on := "enabled"
	if _, err := s.ApplyPatch(LLMSettingsPatch{Thinking: &on, ReasoningEffort: &max}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.ReasoningEffort != "max" {
		t.Fatalf("snap = %+v", snap)
	}
}

func TestRuntimeSettingsApplyPatch_openaiRejected(t *testing.T) {
	s := &RuntimeSettings{Provider: "openai", Model: "gpt-4"}
	off := "disabled"
	_, err := s.ApplyPatch(LLMSettingsPatch{Thinking: &off})
	if err == nil {
		t.Fatal("expected error for openai")
	}
}
