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
		NodeID:      "save-test",
		RuntimeRoot: dir,
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
	cfg := &Config{RuntimeRoot: dir}
	cfg.ApplyDefaults()
	cfg.LLM.Mock = false
	cfg.LLM.Model = ""

	if err := SaveFile(path, cfg); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSaveBootstrapFile_writesListenOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &Config{NodeID: "bootstrap-node"}
	cfg.Listen.Host = "0.0.0.0"
	cfg.Listen.Port = 19001
	cfg.Local.Endpoint = "https://client.example.invalid"
	cfg.Local.NodeID = "client-node"

	if err := SaveBootstrapFile(path, cfg); err != nil {
		t.Fatalf("SaveBootstrapFile: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "listen:") || !strings.Contains(text, "port: 19001") {
		t.Fatalf("bootstrap file missing listener: %s", raw)
	}
	if strings.Contains(text, "local:") || strings.Contains(text, "client.example.invalid") || strings.Contains(text, "client-node") {
		t.Fatalf("bootstrap file contains client configuration: %s", raw)
	}
}
