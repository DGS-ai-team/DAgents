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

func TestHooksExternalEntriesYAML(t *testing.T) {
	path, _ := testConfigPath(t, `
hooks:
  enabled: true
  entries:
    - name: audit-jsonl
      type: journal
      phases: [turn.done, tool.before_each]
      on_error: continue
    - name: compliance-http
      type: http
      url: http://127.0.0.1:9000/hooks
      phases: [tool.before_each]
      timeout_ms: 3000
      on_error: abort
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HooksExternalEnabled() {
		t.Fatal("expected external hooks enabled")
	}
	if len(cfg.Hooks.Entries) != 2 {
		t.Fatalf("entries = %d", len(cfg.Hooks.Entries))
	}
	if cfg.Hooks.Entries[1].OnError != "abort" {
		t.Fatalf("on_error = %q", cfg.Hooks.Entries[1].OnError)
	}
}

func TestHooksExternalEnabledDefaultFalse(t *testing.T) {
	var cfg Config
	if cfg.HooksExternalEnabled() {
		t.Fatal("expected false by default")
	}
}
