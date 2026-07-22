package agenttemplate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveUser_andDeleteUser(t *testing.T) {
	dir := t.TempDir()
	tpl := Template{
		ID:          "my-ops",
		DisplayName: "运维",
		Description: "test",
		Defaults:    map[string]any{"tools": map[string]any{"enabled_groups": []any{"fs"}}},
		Sandbox:     SandboxConfig{Backend: "process"},
	}
	if err := SaveUser(dir, tpl); err != nil {
		t.Fatal(err)
	}
	if !IsUserTemplate(dir, "my-ops") {
		t.Fatal("expected user template")
	}
	loader := NewLoader("", dir)
	got, err := loader.Get("my-ops")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "运维" {
		t.Fatalf("got %+v", got)
	}
	if err := DeleteUser(dir, "my-ops"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "my-ops.yaml")); !os.IsNotExist(err) {
		t.Fatalf("stat err=%v", err)
	}
}

func TestValidateID_rejectsInvalid(t *testing.T) {
	if err := ValidateID("Bad-ID"); err == nil {
		t.Fatal("expected error")
	}
}
