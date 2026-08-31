package store

import (
	"context"
	"path/filepath"
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
