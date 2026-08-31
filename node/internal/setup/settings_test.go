package setup

import (
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testBaseConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		NodeID:      "setup-test",
		RuntimeRoot: t.TempDir(),
	}
	cfg.ApplyDefaults()
	cfg.LLM.Provider = "deepseek"
	cfg.LLM.Model = "deepseek-chat"
	return cfg
}

func TestViewFromConfig(t *testing.T) {
	cfg := testBaseConfig(t)
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "http://127.0.0.1:8020"
	cfg.Manage.Registration.Team = "platform"
	cfg.Skills.Enabled = false

	view := ViewFromConfig(cfg)
	if view.LLM.Provider != "deepseek" || view.Manage.Enabled != true || view.Features.SkillsEnabled != false {
		t.Fatalf("view = %+v", view)
	}
}

func TestApplyPatch_llmProfiles(t *testing.T) {
	cfg := testBaseConfig(t)
	updated, err := ApplyPatch(cfg, SettingsPatch{
		LLM: &LLMSettings{
			Active: "qwen",
			Profiles: []LLMProfileSettings{
				{ID: "default", Provider: "deepseek", Model: "deepseek-chat", APIKeyEnv: "OPENAI_API_KEY", MultimodalEnabled: false},
				{ID: "qwen", Provider: "qwen", Model: "qwen-plus", APIKeyEnv: "QWEN_API_KEY", MultimodalEnabled: true},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LLM.Active != "qwen" || updated.LLM.Provider != "qwen" || updated.LLM.Model != "qwen-plus" {
		t.Fatalf("llm = %+v", updated.LLM)
	}
	if !updated.MultimodalEnabled() {
		t.Fatal("expected multimodal enabled from active profile")
	}
	view := ViewFromConfig(updated)
	if view.LLM.Active != "qwen" || len(view.LLM.Profiles) != 2 {
		t.Fatalf("view llm = %+v", view.LLM)
	}
	var qwen *LLMProfileSettings
	for i := range view.LLM.Profiles {
		if view.LLM.Profiles[i].ID == "qwen" {
			qwen = &view.LLM.Profiles[i]
			break
		}
	}
	if qwen == nil || !qwen.MultimodalEnabled {
		t.Fatalf("qwen profile view = %+v", qwen)
	}
}

func TestApplyPatch_llmAndFeatures(t *testing.T) {
	cfg := testBaseConfig(t)
	updated, err := ApplyPatch(cfg, SettingsPatch{
		LLM: &LLMSettings{
			Provider:  "mock",
			Model:     "mock",
			APIKeyEnv: "TEST_KEY",
			Mock:      true,
		},
		Features: &FeatureSettings{
			SkillsEnabled:     false,
			BrowserEnabled:    true,
			MultimodalEnabled: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.LLM.Mock || updated.LLM.Provider != "mock" {
		t.Fatalf("llm = %+v", updated.LLM)
	}
	if updated.Skills.Enabled != false || !updated.BrowserEnabled() || !updated.MultimodalEnabled() {
		t.Fatalf("features not applied")
	}
}

func TestApplyPatch_manageRequiresURL(t *testing.T) {
	cfg := testBaseConfig(t)
	_, err := ApplyPatch(cfg, SettingsPatch{
		Manage: &ManageSettings{Enabled: true, URL: ""},
	})
	if err == nil {
		t.Fatal("expected error for empty manage.url")
	}
}

func TestApplyPatch_invalidProvider(t *testing.T) {
	cfg := testBaseConfig(t)
	_, err := ApplyPatch(cfg, SettingsPatch{
		LLM: &LLMSettings{Provider: "unknown", Model: "x"},
	})
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestApplyPatch_compression(t *testing.T) {
	cfg := testBaseConfig(t)
	cfg.Compression.SilentTriggerTokens = 80000
	cfg.Compression.BlockingTriggerTokens = 100000

	updated, err := ApplyPatch(cfg, SettingsPatch{
		Compression: &CompressionSettings{
			SilentTriggerTokens:         50000,
			BlockingTriggerTokens:       70000,
			IdleAutoCompressSeconds:     3600,
			IdleAutoCompressPollSeconds: 120,
			IdleAutoCompressMinTokens:   40000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Compression.SilentTriggerTokens != 50000 ||
		updated.Compression.BlockingTriggerTokens != 70000 ||
		updated.Compression.IdleAutoCompressSeconds != 3600 {
		t.Fatalf("compression = %+v", updated.Compression)
	}

	view := ViewFromConfig(updated)
	if view.Compression.SilentTriggerTokens != 50000 {
		t.Fatalf("view compression = %+v", view.Compression)
	}
}

func TestApplyPatch_compressionBlockingLessThanSilent(t *testing.T) {
	cfg := testBaseConfig(t)
	_, err := ApplyPatch(cfg, SettingsPatch{
		Compression: &CompressionSettings{
			SilentTriggerTokens:   100000,
			BlockingTriggerTokens: 50000,
		},
	})
	if err == nil {
		t.Fatal("expected blocking < silent error")
	}
}

func TestApplyPatch_injectTodayDateHook(t *testing.T) {
	cfg := testBaseConfig(t)
	if !cfg.InjectTodayDateHookEnabled() {
		t.Fatal("expected default enabled")
	}
	view := ViewFromConfig(cfg)
	if !view.Hooks.InjectTodayDateEnabled {
		t.Fatal("view should show enabled")
	}
	updated, err := ApplyPatch(cfg, SettingsPatch{
		Hooks: &HooksSettings{
			DuplicateToolCallEnabled:       true,
			DuplicateToolCallWindowSeconds: 60,
			ToolResultEnabled:              true,
			ToolResultSpillThresholdTokens: 12000,
			InjectTodayDateEnabled:         false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.InjectTodayDateHookEnabled() {
		t.Fatal("expected disabled after patch")
	}
	if ViewFromConfig(updated).Hooks.InjectTodayDateEnabled {
		t.Fatal("view should show disabled")
	}
}

func TestApplyPatch_wecomWebhook(t *testing.T) {
	cfg := testBaseConfig(t)
	updated, err := ApplyPatch(cfg, SettingsPatch{
		Features: &FeatureSettings{WeComEnabled: true},
		WeCom: &WeComSettings{
			WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.WeComEnabled() || updated.WeComWebhookKey() != "secret-1" {
		t.Fatalf("wecom=%+v key=%q", updated.WeCom, updated.WeComWebhookKey())
	}
	view := ViewFromConfig(updated)
	if !view.Features.WeComEnabled || !view.WeCom.HasWebhookKey {
		t.Fatalf("view=%+v", view.WeCom)
	}
	if view.WeCom.WebhookKey != "" {
		t.Fatal("GET must not return webhook_key plaintext")
	}
	if strings.Contains(view.WeCom.WebhookURL, "secret-1") {
		t.Fatalf("url should redact key: %s", view.WeCom.WebhookURL)
	}

	cleared, err := ApplyPatch(updated, SettingsPatch{
		Features: &FeatureSettings{WeComEnabled: false},
		WeCom:    &WeComSettings{ClearWebhookKey: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.WeComEnabled() || cleared.WeComWebhookKey() != "" {
		t.Fatal("expected disabled and cleared key")
	}
}

func TestApplyPatch_completeNodeProfile(t *testing.T) {
	cfg := testBaseConfig(t)
	done := false
	cfg.Onboarding.NodeProfileCompleted = &done
	cfg.Agent.Name = ""

	_, err := ApplyPatch(cfg, SettingsPatch{
		Onboarding: &OnboardingSettings{NodeProfileCompleted: true},
	})
	if err == nil {
		t.Fatal("expected error when completing without names")
	}

	updated, err := ApplyPatch(cfg, SettingsPatch{
		User:       &UserSettings{PreferredName: "小明"},
		Agent:      &AgentSettings{Name: "desk-node", Description: "desk"},
		Onboarding: &OnboardingSettings{NodeProfileCompleted: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.NodeProfileCompleted() || updated.PreferredName() != "小明" || updated.Agent.Name != "desk-node" {
		t.Fatalf("updated=%+v user=%+v onboarding=%+v", updated.Agent, updated.User, updated.Onboarding)
	}
	view := ViewFromConfig(updated)
	if !view.Onboarding.NodeProfileCompleted || view.User.PreferredName != "小明" {
		t.Fatalf("view=%+v", view)
	}
}
