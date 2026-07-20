package setup

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testBaseConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		AgentID: "setup-test",
		FSRoot:  t.TempDir(),
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
			SkillsEnabled:   false,
			BrowserEnabled:  true,
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
