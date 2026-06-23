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

func TestHooksPluginsYAML(t *testing.T) {
	path, _ := testConfigPath(t, `
hooks:
  plugins:
    - path: .runtime/plugins/redact.so
      phases: [tool.after_each]
      priority: 100
      on_error: abort
  host:
    max_llm_calls: 3
    history_window: 30
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hooks.Plugins) != 1 {
		t.Fatalf("plugins = %d", len(cfg.Hooks.Plugins))
	}
	if cfg.Hooks.Plugins[0].OnError != "abort" {
		t.Fatalf("on_error = %q", cfg.Hooks.Plugins[0].OnError)
	}
	if cfg.HooksHostMaxLLMCalls() != 3 {
		t.Fatalf("max_llm_calls = %d", cfg.HooksHostMaxLLMCalls())
	}
	if cfg.HooksHostHistoryWindow() != 30 {
		t.Fatalf("history_window = %d", cfg.HooksHostHistoryWindow())
	}
}

func TestHooksHostDefaults(t *testing.T) {
	var cfg Config
	if cfg.HooksHostMaxLLMCalls() != 2 {
		t.Fatalf("max_llm_calls = %d", cfg.HooksHostMaxLLMCalls())
	}
	if cfg.HooksHostHistoryWindow() != 0 {
		t.Fatalf("history_window = %d", cfg.HooksHostHistoryWindow())
	}
}
