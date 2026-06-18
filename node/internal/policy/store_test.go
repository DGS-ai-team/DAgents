package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreApplyToolUpdatesRoundtrip(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policy")
	if err := EnsureRuntimePolicy(dir); err != nil {
		t.Fatal(err)
	}
	if err := ApplyToolUpdates(policyDir, []ToolUpdate{
		{Name: "read_file", Decision: DecisionAllowAuto},
		{Name: "write_file", Decision: DecisionDeny},
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(policyDir, "tool.approval.txt"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "read_file=never") || !strings.Contains(body, "write_file=deny") {
		t.Fatalf("unexpected tool file: %q", body)
	}
	e, err := loadFromDir(policyDir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Decide("write_file") != ActionDeny {
		t.Fatal("write_file should deny after reload")
	}
}

func TestStoreProtectAskUserInformationDeny(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policy")
	if err := EnsureRuntimePolicy(dir); err != nil {
		t.Fatal(err)
	}
	err := ApplyToolUpdates(policyDir, []ToolUpdate{
		{Name: "ask_user_information", Decision: DecisionDeny},
	})
	if err == nil {
		t.Fatal("expected protection error")
	}
}

func TestStoreApplyShellUpdatesRoundtrip(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policy")
	if err := EnsureRuntimePolicy(dir); err != nil {
		t.Fatal(err)
	}
	if err := ApplyShellUpdates(policyDir, ShellBash, []ShellUpdate{
		{Command: "ls", Decision: DecisionAllowAuto},
		{Command: "rm", Decision: DecisionDeny},
	}); err != nil {
		t.Fatal(err)
	}
	e, err := loadFromDir(policyDir)
	if err != nil {
		t.Fatal(err)
	}
	if e.DecideTool("bash_run", map[string]any{"command": "rm x"}) != ActionDeny {
		t.Fatal("rm should deny")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "ls"}) != ActionAuto {
		t.Fatal("ls should auto")
	}
}

func TestLoadSnapshotIncludesRegistryTools(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policy")
	if err := EnsureRuntimePolicy(dir); err != nil {
		t.Fatal(err)
	}
	e, err := loadFromDir(policyDir)
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
