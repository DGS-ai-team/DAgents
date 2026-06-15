package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

func writeTestPolicyDir(t *testing.T, toolContent, bashShellContent string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestPolicyFile(t, dir, "tool.approval.txt", toolContent)
	if bashShellContent != "" {
		writeTestPolicyFile(t, dir, "shell/bash.approval.txt", bashShellContent)
	} else {
		writeTestPolicyFile(t, dir, "shell/bash.approval.txt", "")
		writeTestPolicyFile(t, dir, "shell/cmd.approval.txt", "")
		writeTestPolicyFile(t, dir, "shell/powershell.approval.txt", "")
	}
	return dir
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

func TestPolicyToolHookModes(t *testing.T) {
	dir := writeTestPolicyDir(t,
		"read_file=never\nwrite_file=always\nbash_run=rule\n",
		"echo=never\ngit=always\n",
	)
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewPolicyToolHook(engine)
	ctx := context.Background()

	cases := []struct {
		name   string
		tool   string
		args   map[string]any
		mode   policy.ApprovalMode
		action policy.Action
	}{
		{"never read", "read_file", nil, policy.ModeNever, policy.ActionAuto},
		{"always write", "write_file", map[string]any{"path": "x"}, policy.ModeAlways, policy.ActionRequireApproval},
		{"rule bash auto", "bash_run", map[string]any{"command": "echo ok"}, policy.ModeRule, policy.ActionAuto},
		{"rule bash require", "bash_run", map[string]any{"command": "git status"}, policy.ModeRule, policy.ActionRequireApproval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out ToolBeforeEachResult
			if err := hook.RunToolBeforeEach(ctx, ToolBeforeEachInput{ToolName: tc.tool, ToolArgs: tc.args}, &out); err != nil {
				t.Fatal(err)
			}
			if out.ToolMode != tc.mode {
				t.Fatalf("ToolMode = %q, want %q", out.ToolMode, tc.mode)
			}
			if out.Action != tc.action {
				t.Fatalf("Action = %q, want %q", out.Action, tc.action)
			}
		})
	}
}

func TestRegistrySetPolicyEngine(t *testing.T) {
	engine, _ := policy.LoadFile("")
	reg := NewRegistry(engine, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})

	out := reg.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{ToolName: "read_file"})
	if out.Action != policy.ActionAuto {
		t.Fatalf("before reload Action = %q", out.Action)
	}

	dir := writeTestPolicyDir(t, "read_file=always\n", "")
	reloaded, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetPolicyEngine(reloaded)

	out = reg.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{ToolName: "read_file"})
	if out.Action != policy.ActionRequireApproval {
		t.Fatalf("after reload Action = %q", out.Action)
	}
	if out.ToolMode != policy.ModeAlways {
		t.Fatalf("ToolMode = %q", out.ToolMode)
	}
}

func TestRunToolBeforeEachNilRegistry(t *testing.T) {
	var reg *Registry
	out := reg.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{ToolName: "read_file"})
	if out.Action != policy.ActionRequireApproval {
		t.Fatalf("nil registry should be conservative, got %q", out.Action)
	}
}
