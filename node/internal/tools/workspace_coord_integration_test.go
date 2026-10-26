package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
)

func TestRegistriesShareWorkspaceWriteCoordinator(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "same.txt")
	r1, err := NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	c := workspacecoord.New()
	r1.SetWorkspaceCoordinator(c)
	r2.SetWorkspaceCoordinator(c)
	lease, err := r1.acquireWorkspaceWrite(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	body, _ := json.Marshal(writeFileArgs{Path: target, Content: "blocked"})
	out, err := r2.execWriteFile(ctx, body)
	if err == nil || !strings.Contains(err.Error(), "workspace_busy") || out != "" {
		t.Fatalf("write result=%q err=%v", out, err)
	}
	lease.Release()
	if _, err = r2.Execute(context.Background(), "write_file", string(body)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "blocked" {
		t.Fatalf("target=%q", got)
	}
	other := filepath.Join(root, "other.txt")
	otherBody, _ := json.Marshal(writeFileArgs{Path: other, Content: "independent"})
	lease, err = r1.acquireWorkspaceWrite(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if _, err = r2.Execute(context.Background(), "write_file", string(otherBody)); err != nil {
		t.Fatalf("unrelated write blocked: %v", err)
	}
}

func TestOpenTerminalsDoNotHoldWorkspaceWriteLease(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	registry.SetWorkspaceCoordinator(workspacecoord.New())
	shell := "sh"
	if runtime.GOOS == "windows" {
		shell = "powershell"
	}
	openAndStart := func() Terminal {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		terminal, err := registry.OpenTerminal(ctx, TerminalRequest{
			Target:  ExecutionTarget{Kind: executionTargetLocal},
			Context: ExecutionContext{AgentID: "same-agent"},
			Shell:   shell,
			Rows:    24,
			Cols:    80,
		})
		if err != nil {
			t.Fatalf("open terminal: %v", err)
		}
		if _, err := terminal.Output(); err != nil {
			t.Fatalf("acquire terminal output: %v", err)
		}
		if err := terminal.Start(); err != nil {
			t.Fatalf("start terminal: %v", err)
		}
		return terminal
	}
	first := openAndStart()
	defer first.Close()
	second := openAndStart()
	defer second.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := registry.Execute(ctx, "bash_run", `{"command":"echo workspace-write-check"}`)
		result <- struct {
			out string
			err error
		}{out: out, err: err}
	}()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("bash_run while terminals are open: %v", got.err)
		}
		if !strings.Contains(got.out, "workspace-write-check") {
			t.Fatalf("bash_run output=%q", got.out)
		}
	case <-ctx.Done():
		t.Fatal("bash_run remained blocked while interactive terminals were open")
	}
}
