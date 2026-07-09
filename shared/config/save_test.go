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
		AgentID: "save-test",
		FSRoot:  dir,
	}
	cfg.ApplyDefaults()
	cfg.LLM.Provider = "deepseek"
	cfg.LLM.Model = "deepseek-chat"

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
