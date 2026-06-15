package config

import "testing"

func TestDuplicateToolCallHookConfigDefaults(t *testing.T) {
	var cfg Config
	if !cfg.DuplicateToolCallHookEnabled() {
		t.Fatal("expected enabled by default")
	}
	if cfg.DuplicateToolCallWindowSeconds() != 60 {
		t.Fatalf("window = %d", cfg.DuplicateToolCallWindowSeconds())
	}
}

func TestDuplicateToolCallHookConfigYAML(t *testing.T) {
	path, _ := testConfigPath(t, `
hooks:
  duplicate_tool_call:
    enabled: false
    window_seconds: 120
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DuplicateToolCallHookEnabled() {
		t.Fatal("expected disabled")
	}
	if cfg.DuplicateToolCallWindowSeconds() != 120 {
		t.Fatalf("window = %d", cfg.DuplicateToolCallWindowSeconds())
	}
}
