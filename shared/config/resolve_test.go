package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath_explicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("agent_id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveConfigPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestResolveConfigPath_env(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "from-env.yaml")
	if err := os.WriteFile(path, []byte("agent_id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvConfigPath, path)
	got, err := ResolveConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestResolveConfigPath_missing(t *testing.T) {
	t.Setenv(EnvConfigPath, "")
	if _, err := ResolveConfigPath(""); err == nil {
		t.Fatal("expected error when no config exists")
	}
}
