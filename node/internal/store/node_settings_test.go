package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestNodeSettings_roundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := OpenNodeSettings(filepath.Join(dir, "node_settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	empty, err := s.Empty(context.Background())
	if err != nil || !empty {
		t.Fatalf("empty=%v err=%v", empty, err)
	}

	seed := ProductNodeSettingsSeed()
	seed.RuntimeRoot = dir
	seed.LLM.Active = "profile-a"
	seed.LLM.Provider = "openai"
	seed.LLM.Model = "should-not-be-persisted"
	if err := s.Save(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load(context.Background())
	if err != nil || loaded == nil {
		t.Fatalf("load: %v %#v", err, loaded)
	}
	if !loaded.Skills.Enabled || !loaded.Triggers.Enabled || !loaded.ChildAgents.Enabled {
		t.Fatalf("enabled flags lost: skills=%v triggers=%v child=%v", loaded.Skills.Enabled, loaded.Triggers.Enabled, loaded.ChildAgents.Enabled)
	}
	if loaded.Listen.Port != 0 {
		t.Fatalf("listen should be stripped in snapshot, got %#v", loaded.Listen)
	}
	if loaded.LLM.Active != "profile-a" || loaded.LLM.Provider != "" || loaded.LLM.Model != "" {
		t.Fatalf("node settings should retain only active LLM reference, got %+v", loaded.LLM)
	}
}

func TestBootstrapNodeSettings_seedsProductDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := config.SaveBootstrapFile(cfgPath, &config.Config{
		Listen: config.ListenConfig{Host: "127.0.0.1", Port: 18765},
		Local:  config.LocalConfig{Endpoint: "http://127.0.0.1:18765"},
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeRoot = dir
	ns, err := BootstrapNodeSettings(context.Background(), cfg, cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	if !cfg.Skills.Enabled {
		t.Fatal("expected product seed skills.enabled=true")
	}
	if !cfg.Triggers.Enabled || !cfg.ChildAgents.Enabled {
		t.Fatal("expected triggers/child_agents enabled")
	}
	if cfg.NodeProfileCompleted() {
		t.Fatal("product seed should require first-run node profile")
	}
}

func TestProductSeedToolResultHookToolsMatchSharedDefaultsAndCustomListPersists(t *testing.T) {
	seed := ProductNodeSettingsSeed()
	want := config.DefaultToolResultHookTools()
	if strings.Join(seed.Hooks.ToolResult.Tools, ",") != strings.Join(want, ",") {
		t.Fatalf("seed tools = %v, shared defaults = %v", seed.Hooks.ToolResult.Tools, want)
	}
	dir := t.TempDir()
	settings, err := OpenNodeSettings(filepath.Join(dir, "node_settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	custom := *seed
	custom.Hooks.ToolResult.Tools = []string{"read_file", "custom_tool"}
	if err := settings.Save(context.Background(), &custom); err != nil {
		t.Fatal(err)
	}
	loaded, err := settings.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(loaded.Hooks.ToolResult.Tools, ",") != "read_file,custom_tool" {
		t.Fatalf("custom hook list overwritten: %v", loaded.Hooks.ToolResult.Tools)
	}
}

func TestBootstrapNodeSettingsValidatesAfterDatabaseOverlay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("listen:\n  host: 127.0.0.1\n  port: 18765\nmanage:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("bootstrap YAML should accept stale business values: %v", err)
	}
	cfg.RuntimeRoot = dir
	settings, err := OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	valid := ProductNodeSettingsSeed()
	valid.Manage.Enabled = false
	valid.NodeID = cfg.NodeID
	if err := settings.Save(context.Background(), valid); err != nil {
		settings.Close()
		t.Fatal(err)
	}
	if err := settings.Close(); err != nil {
		t.Fatal(err)
	}

	nodeSettings, err := BootstrapNodeSettings(context.Background(), cfg, cfgPath, nil)
	if err != nil {
		t.Fatalf("DB overlay should make effective config valid: %v", err)
	}
	defer nodeSettings.Close()
	if cfg.Manage.Enabled {
		t.Fatal("expected node_settings.db business value to override stale YAML")
	}
}

func TestBootstrapMigratesLegacyLLMFlatFieldsForFirstProfileSeed(t *testing.T) {
	const keyEnv = "DAGENTS_TEST_LEGACY_LLM_KEY"
	t.Setenv(keyEnv, "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("listen:\n  host: 127.0.0.1\n  port: 18765\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeRoot = dir
	nodeSettings, err := OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	legacy := ProductNodeSettingsSeed()
	legacy.NodeID = cfg.NodeID
	legacy.LLM.Active = "legacy-profile"
	legacy.LLM.Provider = "qwen"
	legacy.LLM.Model = "qwen3-32b"
	legacy.LLM.Thinking = "enabled"
	legacy.LLM.ReasoningEffort = "low"
	legacy.LLM.Profiles = map[string]config.LLMProfileConfig{
		"legacy-profile": {
			Provider: "qwen", Model: "qwen3-32b", APIKeyEnv: keyEnv,
			Thinking: "enabled", ReasoningEffort: "low",
		},
		"secondary": {Provider: "openai", Model: "gpt-test"},
	}
	legacy.LLM.ProfileOrder = []string{"legacy-profile", "secondary"}
	raw, err := json.Marshal(legacy)
	if err != nil {
		nodeSettings.Close()
		t.Fatal(err)
	}
	if _, err := nodeSettings.db.ExecContext(context.Background(), `
INSERT INTO node_settings (id, settings_json, schema_version, updated_at) VALUES (1, ?, 1, '2025-01-01T00:00:00Z')`, string(raw)); err != nil {
		nodeSettings.Close()
		t.Fatal(err)
	}
	if err := nodeSettings.Close(); err != nil {
		t.Fatal(err)
	}

	nodeSettings, err = BootstrapNodeSettings(context.Background(), cfg, cfgPath, nil)
	if err != nil {
		t.Fatalf("bootstrap legacy settings: %v", err)
	}
	defer nodeSettings.Close()
	if cfg.LLM.Provider != "qwen" || cfg.LLM.Model != "qwen3-32b" || cfg.LLM.Active != "legacy-profile" {
		t.Fatalf("legacy profile values not available for first seed: %+v", cfg.LLM)
	}
	compacted, err := nodeSettings.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if compacted.LLM.Active != "legacy-profile" || compacted.LLM.Provider != "qwen" || compacted.LLM.Model != "qwen3-32b" {
		t.Fatalf("legacy source should remain until LLM DB seed succeeds: %+v", compacted.LLM)
	}

	llmStore, err := OpenLLMConfigs(cfg.LLMConfigsDBPath(), cfg.RuntimeDir())
	if err != nil {
		t.Fatal(err)
	}
	defer llmStore.Close()
	if err := EnsureDefaultLLMConfig(context.Background(), llmStore, cfg); err == nil || !strings.Contains(err.Error(), keyEnv) {
		t.Fatalf("missing migration key should be reported: %v", err)
	}
	if count, err := llmStore.Count(context.Background()); err != nil || count != 0 {
		t.Fatalf("failed seed changed LLM DB: count=%d err=%v", count, err)
	}
	compacted, err = nodeSettings.Load(context.Background())
	if err != nil || compacted.LLM.Model != "qwen3-32b" || compacted.LLM.Profiles["legacy-profile"].APIKeyEnv != keyEnv {
		t.Fatalf("failed seed discarded legacy settings: settings=%+v err=%v", compacted, err)
	}
	t.Setenv(keyEnv, "synthetic-legacy-profile-key")
	if err := EnsureDefaultLLMConfig(context.Background(), llmStore, cfg); err != nil {
		t.Fatal(err)
	}
	profiles, err := llmStore.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles[0].ID != "legacy-profile" || profiles[0].Provider != "qwen" || profiles[0].Model != "qwen3-32b" || profiles[1].ID != "secondary" || profiles[1].Model != "gpt-test" {
		t.Fatalf("seeded LLM profile = %+v", profiles)
	}
	key, err := llmStore.ResolveAPIKey(context.Background(), "legacy-profile")
	if err != nil || key != "synthetic-legacy-profile-key" {
		t.Fatalf("legacy profile key = %q, err = %v", key, err)
	}
	if err := nodeSettings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	compacted, err = nodeSettings.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if compacted.LLM.Active != "legacy-profile" || compacted.LLM.Provider != "" || compacted.LLM.Model != "" {
		t.Fatalf("legacy snapshot was not compacted after DB seed: %+v", compacted.LLM)
	}
}
