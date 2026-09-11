package config

import (
	"path/filepath"
	"testing"
)

func TestAgentsDBPath(t *testing.T) {
	cfg := &Config{RuntimeRoot: "/tmp/runtime-x"}
	cfg.ApplyDefaults()
	want := filepath.Join("/tmp/runtime-x", "agents.db")
	if cfg.AgentsDBPath() != want {
		t.Fatalf("AgentsDBPath=%q want %q", cfg.AgentsDBPath(), want)
	}
}
