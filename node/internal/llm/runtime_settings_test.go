package llm

import "testing"

func TestRuntimeSettingsRequestExtraKeepsProviderContracts(t *testing.T) {
	openAI := (&RuntimeSettings{Provider: "openai", Model: "o3", ReasoningEffort: "low"}).RequestExtra()
	if openAI["reasoning_effort"] != "low" || openAI["thinking"] != nil {
		t.Fatalf("OpenAI extra = %#v", openAI)
	}
	deepSeek := (&RuntimeSettings{Provider: "deepseek", Model: "deepseek-v4-pro", Thinking: "enabled", ReasoningEffort: "low"}).RequestExtra()
	if deepSeek["reasoning_effort"] != "low" || deepSeek["thinking"] == nil {
		t.Fatalf("DeepSeek extra = %#v", deepSeek)
	}
	budget := 12000
	qwen := (&RuntimeSettings{Provider: "qwen", Model: "qwen3-32b", Thinking: "enabled", ThinkingBudget: &budget}).RequestExtra()
	if qwen["thinking_budget"] != 12000 || qwen["reasoning_effort"] != nil {
		t.Fatalf("Qwen extra = %#v", qwen)
	}
}

func TestUnknownModelsDoNotReceiveThinkingParameters(t *testing.T) {
	for _, tc := range []struct{ provider, model string }{
		{"openai", "gpt-4o"}, {"deepseek", "custom-model"}, {"qwen", "qwen-coder"}, {"vllm", "local"},
	} {
		if got := BuildRequestExtraForModel(tc.provider, tc.model, "enabled", "high"); got != nil {
			t.Fatalf("%s/%s unexpectedly got request extras: %#v", tc.provider, tc.model, got)
		}
	}
}

func TestModelCapabilitiesAreModelSpecific(t *testing.T) {
	cases := []struct{ provider, model, control string }{
		{"deepseek", "deepseek-v4-flash", ThinkingControlEffort},
		{"deepseek", "deepseek-flash", ThinkingControlEffort},
		{"openai", "gpt-5.1", ThinkingControlEffort},
		{"openai", "gpt-5", ThinkingControlEffort},
		{"openai", "gpt-4o", ""},
		{"qwen", "qwen-plus", ThinkingControlBudget},
		{"qwen", "qwen-flash", ThinkingControlBudget},
		{"qwen", "qwen3-32b", ThinkingControlBudget},
		{"qwen", "qwen3-32b-thinking", ""},
		{"qwen", "qwq-32b", ThinkingControlBudget},
		{"qwen", "qwen-coder", ""},
		{"glm", "glm-4.7", ThinkingControlToggle},
		{"glm", "glm-5.2", ThinkingControlEffort},
		{"glm", "glm-5.3-flash", ThinkingControlEffort},
		{"glm", "glm-4", ""},
		{"mimo", "mimo-v2.5-pro", ThinkingControlToggle},
		{"minimax", "MiniMax-M3", ThinkingControlToggle},
		{"minimax", "MiniMax-M3.1", ""},
		{"minimax", "MiniMax-M3.1-Flash", ThinkingControlEffort},
		{"minimax", "MiniMax-M2.5", ThinkingControlFixed},
	}
	for _, tc := range cases {
		if got := ThinkingControl(tc.provider, tc.model); got != tc.control {
			t.Errorf("%s/%s: got %q, want %q", tc.provider, tc.model, got, tc.control)
		}
	}
	deepseek := ModelCapabilities("deepseek", "deepseek-v4-pro")
	if len(deepseek.AllowedEfforts) != 3 || deepseek.DefaultEffort != "high" || !deepseek.CanDisable {
		t.Fatalf("DeepSeek capability = %+v", deepseek)
	}
	gpt51 := ModelCapabilities("openai", "gpt-5.1")
	if gpt51.AllowedEfforts[0] != "none" || gpt51.AllowedEfforts[len(gpt51.AllowedEfforts)-1] != "high" {
		t.Fatalf("GPT-5.1 capability = %+v", gpt51)
	}
	if ModelCapabilities("qwen", "qwq-32b").CanDisable {
		t.Fatal("QwQ fixed-thinking model must not expose an off switch")
	}
	if ModelCapabilities("glm", "glm-5.2").DefaultEffort != "max" || ModelCapabilities("minimax", "MiniMax-M3.1-Flash").DefaultEffort != "max" {
		t.Fatal("GLM 5.2 and MiniMax M3.1-Flash defaults should be max")
	}
	for _, model := range []string{"glm-5.3", "glm-5.3-flash", "glm-5.3-flashx"} {
		if got := ModelCapabilities("glm", model).DefaultEffort; got != "max" {
			t.Errorf("GLM %s default effort = %q, want max", model, got)
		}
	}
}

func TestBuildRequestExtraUsesModelSpecificEffortAndBudget(t *testing.T) {
	if extra := BuildRequestExtraForModel("deepseek", "deepseek-v4-pro", "enabled", "low"); extra["reasoning_effort"] != "low" {
		t.Fatalf("DeepSeek low effort lost: %#v", extra)
	}
	if extra := BuildRequestExtraForModel("openai", "o4-mini", "enabled", "high"); extra["reasoning_effort"] != "high" || extra["thinking"] != nil {
		t.Fatalf("OpenAI extra = %#v", extra)
	}
	if extra := BuildRequestExtraForModel("deepseek", "deepseek-v4-flash", "disabled", "high"); extra["reasoning_effort"] != nil {
		t.Fatalf("disabled DeepSeek extra = %#v", extra)
	}
	if extra := BuildRequestExtraForModel("minimax", "MiniMax-M3.1-Flash", "disabled", "xhigh"); extra["reasoning_effort"] != "xhigh" || extra["thinking"] != nil {
		t.Fatalf("MiniMax M3.1-Flash extra = %#v", extra)
	}
	budget := 32768
	if extra := BuildRequestExtraForProfile("qwen", "qwen3-32b", "enabled", "", &budget); extra["thinking_budget"] != 32768 {
		t.Fatalf("Qwen extra = %#v", extra)
	}
	if extra := BuildRequestExtraForModel("qwen", "qwen3-32b", "enabled", ""); extra["thinking_budget"] != nil {
		t.Fatalf("empty Qwen budget should use service default: %#v", extra)
	}
	glm := BuildRequestExtraForModel("glm", "glm-5.3", "disabled", "low")
	if glm["reasoning_effort"] != "low" {
		t.Fatalf("GLM effort extra = %#v", glm)
	}
	if thinking, ok := glm["thinking"].(map[string]any); !ok || thinking["type"] != "enabled" {
		t.Fatalf("GLM 5.3 must remain enabled: %#v", glm)
	}
}

func TestValidateThinkingSettingsRejectsInvalidProfileValues(t *testing.T) {
	if err := ValidateThinkingSettings("deepseek", "deepseek-v4-pro", "enabled", "low", nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateThinkingSettings("deepseek", "deepseek-v4-pro", "enabled", "minimal", nil); err == nil {
		t.Fatal("expected invalid effort to be rejected")
	}
	tooLarge := 32769
	if err := ValidateThinkingSettings("qwen", "qwen3-32b", "enabled", "", &tooLarge); err == nil {
		t.Fatal("expected out-of-range budget to be rejected")
	}
	if err := ValidateThinkingSettings("minimax", "MiniMax-M2.5", "disabled", "", nil); err == nil {
		t.Fatal("expected fixed-thinking model to reject disabled value")
	}
	if err := ValidateThinkingSettings("minimax", "MiniMax-M2.5", "off", "", nil); err == nil {
		t.Fatal("expected fixed-thinking model to reject off alias")
	}
}

func TestRuntimeSnapshotIncludesCurrentModelCapability(t *testing.T) {
	snapshot := (&RuntimeSettings{Provider: "openai", Model: "o3", ReasoningEffort: "low"}).Snapshot()
	if !snapshot.ThinkingSupported || snapshot.ReasoningEffort != "low" || snapshot.AllowedEfforts[0] != "low" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	unknown := (&RuntimeSettings{Provider: "openai", Model: "custom-model", Thinking: "enabled"}).Snapshot()
	if unknown.ThinkingSupported || unknown.ThinkingControl != "" || unknown.ReasoningEffort != "" {
		t.Fatalf("unknown snapshot = %+v", unknown)
	}
}
