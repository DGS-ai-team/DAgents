package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSnapshotIncludesRegistryTools(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policy")
	if err := os.MkdirAll(filepath.Join(policyDir, "shell"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyDir, "tool.approval.txt"), []byte("read_file=never\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash.approval.txt", "cmd.approval.txt", "powershell.approval.txt"} {
		if err := os.WriteFile(filepath.Join(policyDir, "shell", name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e, err := LoadFromDir(policyDir)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := LoadSnapshot(policyDir, e, []string{"read_file", "custom_tool"})
	if err != nil {
		t.Fatal(err)
	}
	foundCustom := false
	for _, item := range snap.Tools {
		if item.Name == "custom_tool" {
			foundCustom = true
		}
	}
	if !foundCustom {
		t.Fatal("snapshot should include registry union file tools")
	}
	if snap.Platform.DefaultShell == "" {
		t.Fatal("default_shell required")
	}
}
