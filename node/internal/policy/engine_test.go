package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewDefaultEngine(t *testing.T) {
	e := NewDefaultEngine()
	if e == nil {
		t.Fatal("default engine is nil")
	}
	if e.Decide("read_file") != ActionAuto {
		t.Fatal("read_file should be auto")
	}
	if e.Decide("bash_run") != ActionRequireApproval {
		t.Fatal("bash_run without args should require approval")
	}
	if e.Decide("terminal_command") != ActionRequireApproval {
		t.Fatal("terminal_command should require approval by default")
	}
}

func TestBashRunShellPolicy(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "policy")
	if err := os.MkdirAll(filepath.Join(policyDir, "shell"), 0o755); err != nil {
		t.Fatal(err)
	}
	shellFile := filepath.Join(policyDir, "shell", "bash.approval.txt")
	content := strings.Join([]string{
		"echo=never",
		"rm=always",
	}, "\n")
	if err := os.WriteFile(shellFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	toolFile := filepath.Join(policyDir, "tool.approval.txt")
	if err := os.WriteFile(toolFile, []byte("bash_run=rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := LoadFromDir(policyDir)
	if err != nil {
		t.Fatal(err)
	}
	if e.DecideTool("bash_run", map[string]any{"command": "echo ok", "shell_type": "bash"}) != ActionAuto {
		t.Fatal("echo should auto execute")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "rm -rf /tmp/x", "shell_type": "bash"}) != ActionRequireApproval {
		t.Fatal("rm should require approval")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "echo ok && rm x", "shell_type": "bash"}) != ActionRequireApproval {
		t.Fatal("mixed pipeline should require approval when any segment does")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "", "shell_type": "bash"}) != ActionRequireApproval {
		t.Fatal("empty command should require approval")
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
