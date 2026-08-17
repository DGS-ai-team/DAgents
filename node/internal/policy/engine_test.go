package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileDefaults(t *testing.T) {
	e, err := LoadFile("")
	if err != nil || e == nil {
		t.Fatal(err)
	}
	if e.Decide("read_file") != ActionAuto {
		t.Fatal("read_file should be auto")
	}
	if e.Decide("bash_run") != ActionRequireApproval {
		t.Fatal("bash_run without args should require approval")
	}
	if e.Decide("linux_exec") != ActionRequireApproval {
		t.Fatal("linux_exec should require approval by default")
	}
}

func TestLoadFileLegacyYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	content := "default: deny\ntools:\n  read_file: auto\n  bash_run: auto\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if e.Decide("read_file") != ActionAuto {
		t.Fatal()
	}
	if e.Decide("bash_run") != ActionAuto {
		t.Fatal()
	}
	if e.Decide("write_file") != ActionRequireApproval {
		t.Fatal("unknown yaml tool should fall back to rule/require approval")
	}
}

func TestBashRunShellPolicy(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureRuntimePolicy(dir); err != nil {
		t.Fatal(err)
	}
	shellFile := filepath.Join(dir, "policy", "shell", "bash.approval.txt")
	content := strings.Join([]string{
		"echo=never",
		"rm=always",
	}, "\n")
	if err := os.WriteFile(shellFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	toolFile := filepath.Join(dir, "policy", "tool.approval.txt")
	if err := os.WriteFile(toolFile, []byte("bash_run=rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := loadFromDir(filepath.Join(dir, "policy"))
	if err != nil {
		t.Fatal(err)
	}
	if e.DecideTool("bash_run", map[string]any{"command": "echo ok"}) != ActionAuto {
		t.Fatal("echo should auto execute")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "rm -rf /tmp/x"}) != ActionRequireApproval {
		t.Fatal("rm should require approval")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "echo ok && rm x"}) != ActionRequireApproval {
		t.Fatal("mixed pipeline should require approval when any segment does")
	}
	if e.DecideTool("bash_run", map[string]any{"command": ""}) != ActionRequireApproval {
		t.Fatal("empty command should require approval")
	}
}

func TestLoadRuntimeSeedsFromPackaging(t *testing.T) {
	dir := t.TempDir()
	e, err := LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "policy", "tool.approval.txt")); err != nil {
		t.Fatalf("tool policy not seeded: %v", err)
	}
	if e.DecideTool("bash_run", map[string]any{"command": "echo hi"}) != ActionAuto {
		t.Fatal("seeded bash policy should allow echo")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "git status"}) != ActionRequireApproval {
		t.Fatal("seeded bash policy should require git")
	}
	if e.Decide("linux_exec") != ActionRequireApproval {
		t.Fatal("seeded linux policy should require approval")
	}
}

func TestParseCommandRoots(t *testing.T) {
	roots, ok := ParseCommandRoots(`echo "a && b" && rm x`, ShellBash)
	if !ok || len(roots) != 2 {
		t.Fatalf("roots=%v ok=%v", roots, ok)
	}
	if roots[0] != "echo" || roots[1] != "rm" {
		t.Fatalf("unexpected roots: %v", roots)
	}
}
