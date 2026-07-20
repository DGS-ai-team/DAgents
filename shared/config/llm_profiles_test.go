package config

import "testing"

func TestNormalizeLLMProfiles_fromFlat(t *testing.T) {
	cfg := &Config{}
	cfg.LLM.Provider = "deepseek"
	cfg.LLM.Model = "deepseek-chat"
	cfg.LLM.APIKeyEnv = "OPENAI_API_KEY"
	cfg.ApplyDefaults()
	if cfg.LLM.Active != "default" {
		t.Fatalf("active = %q", cfg.LLM.Active)
	}
	p, ok := cfg.LLM.GetProfile("default")
	if !ok || p.Provider != "deepseek" || p.Model != "deepseek-chat" {
		t.Fatalf("profile = %+v ok=%v", p, ok)
	}
}

func TestSetActiveLLMProfile(t *testing.T) {
	cfg := &Config{}
	cfg.ApplyDefaults()
	if err := cfg.UpsertProfile("qwen", LLMProfileConfig{
		Provider:  "qwen",
		Model:     "qwen-plus",
		APIKeyEnv: "QWEN_API_KEY",
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetActiveLLMProfile("qwen"); err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Provider != "qwen" || cfg.LLM.Model != "qwen-plus" || cfg.LLM.Active != "qwen" {
		t.Fatalf("llm = %+v", cfg.LLM)
	}
}

func TestDeleteProfile_switchesActive(t *testing.T) {
	cfg := &Config{}
	cfg.LLM.Profiles = map[string]LLMProfileConfig{
		"a": {Provider: "openai", Model: "m1", APIKeyEnv: "OPENAI_API_KEY"},
		"b": {Provider: "deepseek", Model: "m2", APIKeyEnv: "OPENAI_API_KEY"},
	}
	cfg.LLM.Active = "a"
	cfg.ApplyDefaults()
	if err := cfg.DeleteProfile("a"); err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Active != "b" {
		t.Fatalf("active = %q, want b", cfg.LLM.Active)
	}
	if cfg.LLM.Provider != "deepseek" {
		t.Fatalf("provider = %q", cfg.LLM.Provider)
	}
}

func TestDeleteLastProfileRejected(t *testing.T) {
	cfg := &Config{}
	cfg.ApplyDefaults()
	if err := cfg.DeleteProfile(cfg.LLM.Active); err == nil {
		t.Fatal("expected error deleting last profile")
	}
}

func TestLLMProfile_multimodalFollowsActive(t *testing.T) {
	cfg := &Config{}
	on := true
	off := false
	cfg.LLM.Profiles = map[string]LLMProfileConfig{
		"text":   {Provider: "deepseek", Model: "deepseek-chat", APIKeyEnv: "OPENAI_API_KEY", MultimodalEnabled: &off},
		"vision": {Provider: "openai", Model: "gpt-4o", APIKeyEnv: "OPENAI_API_KEY", MultimodalEnabled: &on},
	}
	cfg.LLM.Active = "text"
	cfg.ApplyDefaults()
	if cfg.MultimodalEnabled() {
		t.Fatal("text profile should disable multimodal")
	}
	if err := cfg.SetActiveLLMProfile("vision"); err != nil {
		t.Fatal(err)
	}
	if !cfg.MultimodalEnabled() {
		t.Fatal("vision profile should enable multimodal")
	}
	p, ok := cfg.LLM.GetProfile("vision")
	if !ok || !ProfileMultimodalEnabled(p) {
		t.Fatalf("vision profile = %+v", p)
	}
}

func TestMigrateMultimodalIntoProfiles(t *testing.T) {
	on := true
	cfg := &Config{}
	cfg.Multimodal.Enabled = &on
	cfg.LLM.Profiles = map[string]LLMProfileConfig{
		"default": {Provider: "openai", Model: "gpt-4o", APIKeyEnv: "OPENAI_API_KEY"},
	}
	cfg.LLM.Active = "default"
	cfg.ApplyDefaults()
	p, ok := cfg.LLM.GetProfile("default")
	if !ok || !ProfileMultimodalEnabled(p) {
		t.Fatalf("expected legacy multimodal.enabled migrated onto profile, got %+v", p)
	}
	if !cfg.MultimodalEnabled() {
		t.Fatal("expected multimodal still enabled after normalize")
	}
}
