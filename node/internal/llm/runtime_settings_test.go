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

func TestBuildRequestExtra_qwen(t *testing.T) {
	extra := BuildRequestExtra("qwen", "enabled", "max")
	if extra["enable_thinking"] != true {
		t.Fatalf("enable_thinking = %v", extra["enable_thinking"])
	}
	if extra["thinking_budget"] != 32768 {
		t.Fatalf("thinking_budget = %v", extra["thinking_budget"])
	}
	disabled := BuildRequestExtra("qwen", "disabled", "max")
	if disabled["enable_thinking"] != false {
		t.Fatalf("disabled = %v", disabled)
	}
	if _, ok := disabled["thinking_budget"]; ok {
		t.Fatalf("disabled should not set budget: %v", disabled)
	}
}

func TestRuntimeSettingsApplyPatch_openaiRejected(t *testing.T) {
	for _, provider := range []string{"openai", "vllm"} {
		s := &RuntimeSettings{Provider: provider, Model: "gpt-4"}
		off := "disabled"
		_, err := s.ApplyPatch(LLMSettingsPatch{Thinking: &off})
		if err == nil {
			t.Fatalf("expected error for %s", provider)
		}
	}
}

func TestRuntimeSettingsApplyPatch_qwen(t *testing.T) {
	s := &RuntimeSettings{Provider: "qwen", Model: "qwen-plus", Thinking: "enabled", ReasoningEffort: "high"}
	off := "disabled"
	view, err := s.ApplyPatch(LLMSettingsPatch{Thinking: &off})
	if err != nil {
		t.Fatal(err)
	}
	if view.Thinking != "disabled" || !view.ThinkingSupported {
		t.Fatalf("view = %+v", view)
	}
}

func TestThinkingSupported(t *testing.T) {
	for _, p := range []string{"deepseek", "qwen"} {
		if !ThinkingSupported(p) {
			t.Fatalf("%s should support thinking", p)
		}
	}
	for _, p := range []string{"openai", "vllm", ""} {
		if ThinkingSupported(p) {
			t.Fatalf("%s should not support thinking", p)
		}
	}
}
