package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveFile_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &Config{
		NodeID: "save-test",
		FSRoot: dir,
	}
	cfg.LLM.Provider = "deepseek"
	cfg.LLM.Model = "deepseek-chat"
	cfg.ApplyDefaults()

	if err := SaveFile(path, cfg); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "deepseek") {
		t.Fatalf("saved yaml missing provider: %s", raw)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.LLM.Provider != "deepseek" || loaded.LLM.Model != "deepseek-chat" {
		t.Fatalf("loaded llm = %+v", loaded.LLM)
	}
	if loaded.LLM.Active != "default" {
		t.Fatalf("active = %q", loaded.LLM.Active)
	}
}

func TestSaveFile_rejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &Config{FSRoot: dir}
	cfg.ApplyDefaults()
	cfg.LLM.Mock = false
	cfg.LLM.Model = ""

	if err := SaveFile(path, cfg); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestFileHasMigratableSettings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	thin := filepath.Join(dir, "thin.yaml")
	if err := SaveBootstrapFile(thin, &Config{
		Listen: ListenConfig{Host: "127.0.0.1", Port: 18765},
		Local:  LocalConfig{Endpoint: "http://127.0.0.1:18765"},
	}); err != nil {
		t.Fatal(err)
	}
	if FileHasMigratableSettings(thin) {
		t.Fatal("bootstrap-only yaml should not be migratable")
	}
	fat := filepath.Join(dir, "fat.yaml")
	if err := os.WriteFile(fat, []byte("listen:\n  port: 1\nskills:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !FileHasMigratableSettings(fat) {
		t.Fatal("fat yaml should be migratable")
	}
}
