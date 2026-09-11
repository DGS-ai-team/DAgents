package config

import "testing"

func TestNodeProfileCompleted(t *testing.T) {
	cfg := &Config{}
	cfg.Onboarding.NodeProfileCompleted = false
	if cfg.NodeProfileCompleted() {
		t.Fatal("false should gate")
	}
	cfg.Onboarding.NodeProfileCompleted = true
	if !cfg.NodeProfileCompleted() {
		t.Fatal("true should complete")
	}
}

func TestPreferredName(t *testing.T) {
	cfg := &Config{User: UserConfig{PreferredName: "  Ada  "}}
	if got := cfg.PreferredName(); got != "Ada" {
		t.Fatalf("PreferredName=%q", got)
	}
}
