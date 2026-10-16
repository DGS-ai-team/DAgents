package setup

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testBaseConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		NodeID:      "setup-test",
		RuntimeRoot: t.TempDir(),
		LLM:         config.LLMConfig{Provider: "deepseek", Model: "deepseek-chat"},
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestViewFromConfig(t *testing.T) {
	cfg := testBaseConfig(t)
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "http://127.0.0.1:8020"
	cfg.Manage.NodeToken = "manage-secret"
	cfg.Manage.Registration.Team = "platform"
	cfg.Skills.Enabled = false

	view := ViewFromConfig(cfg)
	if len(view.LLM.Profiles) == 0 || view.LLM.Profiles[0].Provider != "deepseek" || view.Manage.Enabled != true || view.Features.SkillsEnabled != false {
		t.Fatalf("view = %+v", view)
	}
	if !view.Manage.HasNodeToken {
		t.Fatalf("Manage token view must be redacted with presence flag: %+v", view.Manage)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "manage-secret") || strings.Contains(string(raw), `"local_endpoint"`) {
		t.Fatalf("settings view leaked secret or Desktop endpoint: %s", raw)
	}
}

func TestApplyPatch_nodeTokenIsWriteOnlyPreservingAndExplicitlyClearable(t *testing.T) {
	cfg := testBaseConfig(t)
	workgroupEnabled := false
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "https://manage.example.test"
	cfg.Manage.Registration.Team = "keep-team"
	cfg.Manage.Registration.BaseURL = "https://register.example.test"
	cfg.Manage.Registration.IntervalSeconds = 90
	cfg.Manage.Registration.TTLSeconds = 180
	cfg.Manage.Workgroup.Enabled = &workgroupEnabled
	cfg.Manage.NodeToken = "existing-secret"

	view := ViewFromConfig(cfg)
	if !view.Manage.HasNodeToken {
		t.Fatalf("GET view = %+v", view.Manage)
	}

	emptyToken := ""
	updated, err := ApplyPatch(cfg, SettingsPatch{Manage: &ManageSettingsPatch{NodeToken: &emptyToken}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Manage.NodeToken != "existing-secret" {
		t.Fatal("empty write-only token should preserve the saved token")
	}

	newToken := "new-secret"
	updated, err = ApplyPatch(updated, SettingsPatch{Manage: &ManageSettingsPatch{NodeToken: &newToken}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Manage.NodeToken != "new-secret" {
		t.Fatal("non-empty token should replace the saved token")
	}
	assertManageSettingsPreserved(t, updated)
	updated, err = ApplyPatch(updated, SettingsPatch{Manage: &ManageSettingsPatch{ClearNodeToken: true}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Manage.NodeToken != "" || ViewFromConfig(updated).Manage.HasNodeToken {
		t.Fatal("explicit clear should remove the saved token")
	}
	assertManageSettingsPreserved(t, updated)
}

func assertManageSettingsPreserved(t *testing.T, cfg *config.Config) {
	t.Helper()
	if !cfg.Manage.Enabled || cfg.Manage.URL != "https://manage.example.test" || cfg.Manage.Registration.Team != "keep-team" || cfg.Manage.Registration.BaseURL != "https://register.example.test" || cfg.Manage.Registration.IntervalSeconds != 90 || cfg.Manage.Registration.TTLSeconds != 180 || cfg.Manage.Workgroup.Enabled == nil || *cfg.Manage.Workgroup.Enabled {
		t.Fatalf("token-only patch changed Manage settings: %+v", cfg.Manage)
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
			Profiles: []LLMProfileSettings{{ID: "default", Provider: "mock", Model: "mock", APIKeyEnv: "TEST_KEY", Mock: true}},
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
	enabled := true
	emptyURL := ""
	_, err := ApplyPatch(cfg, SettingsPatch{
		Manage: &ManageSettingsPatch{Enabled: &enabled, URL: &emptyURL},
	})
	if err == nil {
		t.Fatal("expected error for empty manage.url")
	}
}

func TestApplyPatch_invalidProvider(t *testing.T) {
	cfg := testBaseConfig(t)
	_, err := ApplyPatch(cfg, SettingsPatch{
		LLM: &LLMSettings{Profiles: []LLMProfileSettings{{ID: "default", Provider: "unknown", Model: "x"}}},
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
			SilentTriggerTokens:        50000,
			BlockingTriggerTokens:      70000,
			IdleNewConversationSeconds: 3600,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Compression.SilentTriggerTokens != 50000 ||
		updated.Compression.BlockingTriggerTokens != 70000 ||
		updated.Compression.IdleNewConversationSeconds != 3600 {
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

func TestApplyPatch_memoryCandidatePipeline(t *testing.T) {
	cfg := testBaseConfig(t)
	updated, err := ApplyPatch(cfg, SettingsPatch{Memory: &MemorySettings{
		AutoExtract:        true,
		CandidateQueueSize: 4,
		MaxCandidates:      3,
		CoreBudgetTokens:   900,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Memory.AutoExtract || updated.Memory.CandidateQueueSize != 4 || updated.Memory.MaxCandidates != 3 || updated.Memory.CoreBudgetTokens != 900 {
		t.Fatalf("memory config = %+v", updated.Memory)
	}
	view := ViewFromConfig(updated)
	if view.Memory != (MemorySettings{AutoExtract: true, CandidateQueueSize: 4, MaxCandidates: 3, CoreBudgetTokens: 900}) {
		t.Fatalf("memory view = %+v", view.Memory)
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
	const missingKeyEnv = "DAGENTS_TEST_SETUP_PROFILE_MISSING_KEY"
	t.Setenv(missingKeyEnv, "")
	cfg := testBaseConfig(t)
	cfg.Onboarding.NodeProfileCompleted = false
	cfg.Agent.Name = ""
	cfg.LLM.Mock = false
	cfg.LLM.APIKeyEnv = missingKeyEnv

	_, err := ApplyPatch(cfg, SettingsPatch{
		Onboarding: &OnboardingSettings{NodeProfileCompleted: true},
	})
	if err == nil {
		t.Fatal("expected error when completing without a Node name")
	}

	updated, err := ApplyPatch(cfg, SettingsPatch{
		Agent:      &AgentSettings{Name: "desk-node", Description: "desk"},
		Onboarding: &OnboardingSettings{NodeProfileCompleted: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.NodeProfileCompleted() || updated.PreferredName() != "" || updated.Agent.Name != "desk-node" {
		t.Fatalf("updated=%+v user=%+v onboarding=%+v", updated.Agent, updated.User, updated.Onboarding)
	}
	if updated.LLM.Mock {
		t.Fatal("test setup should not use a mock LLM")
	}
	view := ViewFromConfig(updated)
	if !view.Onboarding.NodeProfileCompleted || view.User.PreferredName != "" {
		t.Fatalf("view=%+v", view)
	}
}
