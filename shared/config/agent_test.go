package config

import "testing"

func TestNodeProfileCompleted_legacyNil(t *testing.T) {
	cfg := &Config{}
	if !cfg.NodeProfileCompleted() {
		t.Fatal("nil onboarding flag should be completed (legacy)")
	}
	done := false
	cfg.Onboarding.NodeProfileCompleted = &done
	if cfg.NodeProfileCompleted() {
		t.Fatal("explicit false should gate")
	}
	done = true
	if !cfg.NodeProfileCompleted() {
		t.Fatal("explicit true should complete")
	}
}

func TestPreferredName(t *testing.T) {
	cfg := &Config{User: UserConfig{PreferredName: "  Ada  "}}
	if got := cfg.PreferredName(); got != "Ada" {
		t.Fatalf("PreferredName=%q", got)
	}
}
