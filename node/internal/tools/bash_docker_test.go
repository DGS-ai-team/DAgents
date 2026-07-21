package tools

import (
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/sandbox"
)

func TestRegistry_dockerStartShellCommand(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := sandbox.NewDockerRunner(root, sandbox.Spec{Image: "alpine:3.20", Network: "bridge"})
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDockerSandbox(runner)

	cmd, err := reg.startShellCommand(shellRunParams{
		command:   "echo hi",
		cwd:       root,
		shellType: shellBash,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "docker run") || !strings.Contains(joined, "alpine:3.20") {
		t.Fatalf("args=%v", cmd.Args)
	}
	if !strings.Contains(joined, "--network bridge") {
		t.Fatalf("network missing: %v", cmd.Args)
	}
}

func TestPrepareShellRun_dockerForcesBash(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := sandbox.NewDockerRunner(root, sandbox.Spec{})
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDockerSandbox(runner)

	ps := "powershell"
	_, msg, err := reg.prepareShellRun(bashRunArgs{Command: "echo 1", ShellType: &ps})
	if err != nil {
		t.Fatal(err)
	}
	if msg == "" || !strings.Contains(msg, "bash") {
		t.Fatalf("msg=%q", msg)
	}
}
