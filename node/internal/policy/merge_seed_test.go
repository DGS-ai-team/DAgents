package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeMissingToolModes(t *testing.T) {
	dst := map[string]string{
		"write_file": "deny",
		"read_file":  "rule",
	}
	seed := map[string]ApprovalMode{
		"write_file":       ModeRule,
		"browser_run_task": ModeAlways,
		"read_file":        ModeNever,
	}
	out, added := MergeMissingToolModes(dst, seed)
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	if out["write_file"] != "deny" {
		t.Fatalf("must not overwrite write_file: %q", out["write_file"])
	}
	if out["browser_run_task"] != "always" {
		t.Fatalf("browser_run_task = %q", out["browser_run_task"])
	}
	_, added2 := MergeMissingToolModes(out, seed)
	if added2 != 0 {
		t.Fatalf("idempotent added = %d", added2)
	}
}

func TestMergeMissingSeedToolsIntoFile(t *testing.T) {
	dir := t.TempDir()
	toolFile := filepath.Join(dir, "tool.approval.txt")
	initial := "# keep me\nwrite_file=deny\nbrowser_navigate=always\n"
	if err := os.WriteFile(toolFile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	seed := map[string]ApprovalMode{
		"write_file":          ModeRule,
		"browser_navigate":    ModeNever,
		"browser_run_task":    ModeAlways,
		"browser_task_status": ModeNever,
	}
	n, err := MergeMissingSeedToolsIntoFile(toolFile, seed)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("added = %d, want 2", n)
	}
	raw, err := os.ReadFile(toolFile)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "# keep me") {
		t.Fatal("comments should be preserved")
	}
	if !strings.Contains(body, "write_file=deny") {
		t.Fatal("existing mode must stay")
	}
	if !strings.Contains(body, "browser_run_task=always") || !strings.Contains(body, "browser_task_status=never") {
		t.Fatalf("missing seed keys: %s", body)
	}
	if !strings.Contains(body, seedMergeMarker) {
		t.Fatal("expected merge marker")
	}
	n2, err := MergeMissingSeedToolsIntoFile(toolFile, seed)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("second merge added = %d", n2)
	}
}

func TestEnsureRuntimePolicyMergesMissingSeedTools(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policy")
	if err := os.MkdirAll(filepath.Join(policyDir, "shell"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 模拟升级前的旧 tool.approval（无 browser_run_task）。
	old := "write_file=deny\nbrowser_navigate=always\n"
	if err := os.WriteFile(filepath.Join(policyDir, "tool.approval.txt"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash.approval.txt", "cmd.approval.txt", "powershell.approval.txt"} {
		if err := os.WriteFile(filepath.Join(policyDir, "shell", name), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureRuntimePolicy(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(policyDir, "tool.approval.txt"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "write_file=deny") {
		t.Fatal("user mode overwritten")
	}
	if !strings.Contains(body, "browser_run_task=always") {
		t.Fatalf("expected browser_run_task merge, got:\n%s", body)
	}
}
