package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModeDenyParsing(t *testing.T) {
	dir := t.TempDir()
	writeTestPolicyFile(t, dir, "tool.approval.txt", "write_file=deny\nread_file=never\n")
	e, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Decide("write_file") != ActionDeny {
		t.Fatal("write_file should be denied")
	}
	if e.Decide("read_file") != ActionAuto {
		t.Fatal("read_file should auto")
	}
}

func TestDecideToolExplicitNeverOverridesFallback(t *testing.T) {
	dir := t.TempDir()
	writeTestPolicyFile(t, dir, "tool.approval.txt", "trigger_update=never\n")
	e, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Decide("trigger_update") != ActionAuto {
		t.Fatal("explicit never should override trigger fallback")
	}
}

func TestDecideToolExplicitDenyOverridesFallback(t *testing.T) {
	dir := t.TempDir()
	writeTestPolicyFile(t, dir, "tool.approval.txt", "trigger_list=deny\n")
	e, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Decide("trigger_list") != ActionDeny {
		t.Fatal("explicit deny should override trigger_list auto fallback")
	}
}

func TestBashDenyPriority(t *testing.T) {
	dir := t.TempDir()
	writeTestPolicyFile(t, dir, "tool.approval.txt", "bash_run=rule\n")
	writeTestPolicyFile(t, dir, "shell/bash.approval.txt", "echo=never\nrm=deny\n")
	e, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.DecideTool("bash_run", map[string]any{"command": "rm -rf /tmp/x", "shell_type": "bash"}) != ActionDeny {
		t.Fatal("rm should deny")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "echo ok && rm x", "shell_type": "bash"}) != ActionDeny {
		t.Fatal("mixed pipeline with deny segment should deny")
	}
	if e.DecideTool("bash_run", map[string]any{"command": "echo ok", "shell_type": "bash"}) != ActionAuto {
		t.Fatal("echo should auto")
	}
}

func TestMappedDeny(t *testing.T) {
	e := NewEngineFromMaps(Maps{Tools: map[string]ApprovalMode{"write_file": ModeDeny}})
	if e.Decide("write_file") != ActionDeny {
		t.Fatal("mapped deny should remain ModeDeny")
	}
}

func writeTestPolicyFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
