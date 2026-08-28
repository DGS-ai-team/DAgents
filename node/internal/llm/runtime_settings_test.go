package llm

import "testing"

func TestRuntimeSettings_RequestExtra_includesUserID(t *testing.T) {
	s := &RuntimeSettings{AgentID: "agent-abc", Provider: "openai", Model: "gpt-4"}
	extra := s.RequestExtra()
	if extra == nil || extra["user_id"] != "agent-abc" {
		t.Fatalf("extra = %v", extra)
	}
}

func TestRuntimeSettings_RequestExtra_userIDWithDeepSeekThinking(t *testing.T) {
	s := &RuntimeSettings{AgentID: "agent-xyz", Provider: "deepseek", Thinking: "enabled", ReasoningEffort: "high"}
	extra := s.RequestExtra()
	if extra["user_id"] != "agent-xyz" {
		t.Fatalf("user_id = %v", extra["user_id"])
	}
	if extra["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v", extra["reasoning_effort"])
	}
}

func TestBuildRequestExtra_deepseek(t *testing.T) {
	extra := BuildRequestExtraForModel("deepseek", "", "enabled", "max")
	if extra["reasoning_effort"] != "max" {
		t.Fatalf("effort = %v", extra["reasoning_effort"])
	}
	disabled := BuildRequestExtraForModel("deepseek", "", "disabled", "max")
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
	extra := BuildRequestExtraForModel("qwen", "", "enabled", "max")
	if extra["enable_thinking"] != true {
		t.Fatalf("enable_thinking = %v", extra["enable_thinking"])
	}
	if extra["thinking_budget"] != 32768 {
		t.Fatalf("thinking_budget = %v", extra["thinking_budget"])
	}
	disabled := BuildRequestExtraForModel("qwen", "", "disabled", "max")
	if disabled["enable_thinking"] != false {
		t.Fatalf("disabled = %v", disabled)
	}
	if _, ok := disabled["thinking_budget"]; ok {
		t.Fatalf("disabled should not set budget: %v", disabled)
	}
}

func TestRuntimeSettingsApplyPatch_vllmRejected(t *testing.T) {
	s := &RuntimeSettings{Provider: "vllm", Model: "local"}
	off := "disabled"
	_, err := s.ApplyPatch(LLMSettingsPatch{Thinking: &off})
	if err == nil {
		t.Fatal("expected error for vllm")
	}
}

func TestRuntimeSettingsApplyPatch_openai(t *testing.T) {
	s := &RuntimeSettings{Provider: "openai", Model: "gpt-4", Thinking: "enabled", ReasoningEffort: "high"}
	off := "disabled"
	view, err := s.ApplyPatch(LLMSettingsPatch{Thinking: &off})
	if err != nil {
		t.Fatal(err)
	}
	if view.Thinking != "disabled" || !view.ThinkingSupported {
		t.Fatalf("view = %+v", view)
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
	for _, p := range []string{"deepseek", "qwen", "openai", "glm", "minimax", "mimo"} {
		if !ThinkingSupported(p) {
			t.Fatalf("%s should support thinking", p)
		}
	}
	for _, p := range []string{"vllm", ""} {
		if ThinkingSupported(p) {
			t.Fatalf("%s should not support thinking", p)
		}
	}
}

func TestReasoningEffortSupported(t *testing.T) {
	for _, p := range []string{"deepseek", "qwen", "openai"} {
		if !ReasoningEffortSupported(p) {
			t.Fatalf("%s should support reasoning effort", p)
		}
	}
	for _, p := range []string{"glm", "minimax", "mimo", "vllm"} {
		if ReasoningEffortSupported(p) {
			t.Fatalf("%s should not support reasoning effort", p)
		}
	}
}

func TestThinkingControl_providerAndModel(t *testing.T) {
	cases := []struct {
		provider string
		model    string
		want     string
	}{
		{provider: "deepseek", model: "deepseek-chat", want: ThinkingControlEffort},
		{provider: "openai", model: "gpt-5", want: ThinkingControlEffort},
		{provider: "qwen", model: "qwen-plus", want: ThinkingControlBudget},
		{provider: "glm", model: "glm-5.2", want: ThinkingControlToggle},
		{provider: "mimo", model: "mimo-v2.5-pro", want: ThinkingControlToggle},
		{provider: "minimax", model: "MiniMax-M3", want: ThinkingControlToggle},
		{provider: "minimax", model: "MiniMax-M2.5", want: ThinkingControlFixed},
		{provider: "vllm", model: "local", want: ""},
	}
	for _, tc := range cases {
		if got := ThinkingControl(tc.provider, tc.model); got != tc.want {
			t.Fatalf("provider=%s model=%s got %q want %q", tc.provider, tc.model, got, tc.want)
		}
	}
}

func TestRuntimeSettingsSnapshot_exposesThinkingControl(t *testing.T) {
	qwen := (&RuntimeSettings{Provider: "qwen", Model: "qwen-plus", Thinking: "enabled", ReasoningEffort: "high"}).Snapshot()
	if qwen.ThinkingControl != ThinkingControlBudget || qwen.ThinkingSecondaryLabel != "思考预算" {
		t.Fatalf("qwen snapshot = %+v", qwen)
	}

	minimax := (&RuntimeSettings{Provider: "minimax", Model: "MiniMax-M2.5", Thinking: "disabled"}).Snapshot()
	if minimax.ThinkingControl != ThinkingControlFixed || minimax.ThinkingLabel != "思考" || minimax.Thinking != "enabled" {
		t.Fatalf("minimax snapshot = %+v", minimax)
	}
}

func TestBuildRequestExtra_newProviders(t *testing.T) {
	cases := []struct {
		provider string
		wantKey  string
	}{
		{provider: "glm", wantKey: "clear_thinking"},
		{provider: "minimax", wantKey: "reasoning_split"},
		{provider: "mimo", wantKey: "thinking"},
	}
	for _, tc := range cases {
		extra := BuildRequestExtraForModel(tc.provider, "", "enabled", "high")
		if tc.provider == "glm" {
			thinking, ok := extra["thinking"].(map[string]any)
			if !ok || thinking[tc.wantKey] != false {
				t.Fatalf("provider=%s extra=%v missing preserved thinking flag", tc.provider, extra)
			}
			continue
		}
		if extra[tc.wantKey] == nil {
			t.Fatalf("provider=%s extra=%v missing %s", tc.provider, extra, tc.wantKey)
		}
	}
	if got := BuildRequestExtraForModel("mimo", "", "disabled", "high")["thinking"].(map[string]string)["type"]; got != "disabled" {
		t.Fatalf("mimo thinking type = %q", got)
	}
}

func TestRuntimeSettingsApplyPatch_rejectsUnsupportedReasoningEffort(t *testing.T) {
	s := &RuntimeSettings{Provider: "mimo", Model: "mimo-v2.5-pro", Thinking: "enabled"}
	effort := "max"
	if _, err := s.ApplyPatch(LLMSettingsPatch{ReasoningEffort: &effort}); err == nil {
		t.Fatal("expected reasoning effort to be rejected for mimo")
	}
}

func TestRuntimeSettingsApplyPatch_rejectsFixedThinkingModel(t *testing.T) {
	s := &RuntimeSettings{Provider: "minimax", Model: "MiniMax-M2.5", Thinking: "enabled"}
	off := "disabled"
	if _, err := s.ApplyPatch(LLMSettingsPatch{Thinking: &off}); err == nil {
		t.Fatal("expected fixed thinking model to reject toggle")
	}
	if got := s.Snapshot().Thinking; got != "enabled" {
		t.Fatalf("thinking changed after rejected patch: %q", got)
	}
}

func TestBuildRequestExtraForModel_minimaxThinkingModes(t *testing.T) {
	fixed := BuildRequestExtraForModel("minimax", "MiniMax-M2.5", "disabled", "high")
	if got := fixed["thinking"].(map[string]string)["type"]; got != "adaptive" {
		t.Fatalf("fixed model thinking type = %q", got)
	}

	toggle := BuildRequestExtraForModel("minimax", "MiniMax-M3", "disabled", "high")
	if got := toggle["thinking"].(map[string]string)["type"]; got != "disabled" {
		t.Fatalf("M3 thinking type = %q", got)
	}
}
