package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveNodeID_migratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{FSRoot: dir}
	cfg.ApplyDefaults()
	legacy := filepath.Join(dir, "agent", "agent_id")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("legacy-node"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ResolveNodeID(); err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "legacy-node" {
		t.Fatalf("NodeID=%q", cfg.NodeID)
	}
	raw, err := os.ReadFile(cfg.NodeIDFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "legacy-node" {
		t.Fatalf("new file=%q", raw)
	}
}

func TestApplyDefaults_mergesLegacyAgentIDYAML(t *testing.T) {
	cfg := &Config{LegacyAgentID: "from-yaml-agent-id", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	if cfg.NodeID != "from-yaml-agent-id" {
		t.Fatalf("NodeID=%q", cfg.NodeID)
	}
	if cfg.LegacyAgentID != "" {
		t.Fatalf("LegacyAgentID should be cleared, got %q", cfg.LegacyAgentID)
	}
}

func TestAgentsDBPath(t *testing.T) {
	cfg := &Config{FSRoot: "/tmp/runtime-x"}
	cfg.ApplyDefaults()
	want := filepath.Join("/tmp/runtime-x", "agents.db")
	if cfg.AgentsDBPath() != want {
		t.Fatalf("AgentsDBPath=%q want %q", cfg.AgentsDBPath(), want)
	}
}
