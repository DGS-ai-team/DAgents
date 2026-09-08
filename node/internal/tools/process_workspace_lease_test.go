package tools

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
)

func TestBashProcessRetainsWorkspaceLeaseUntilExit(t *testing.T) {
	r, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := NewRegistry(r.WorkspaceRoot(), 5)
	if err != nil {
		t.Fatal(err)
	}
	c := workspacecoord.New()
	r.SetWorkspaceCoordinator(c)
	r2.SetWorkspaceCoordinator(c)
	command := "Start-Sleep -Milliseconds 700"
	if runtime.GOOS != "windows" {
		command = "sleep 1"
	}
	ctx := WithToolCallID(WithSession(context.Background(), "lease"), "lease-1")
	done := make(chan struct{})
	go func() {
		out, runErr := r.Execute(ctx, "bash_run", `{"command":"`+command+`","timeout_seconds":3}`)
		if runErr != nil || strings.Contains(out, "ERROR") {
			t.Errorf("first process out=%q err=%v", out, runErr)
		}
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for r.SessionToolJobCounts("lease").Running < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.SessionToolJobCounts("lease").Running < 1 {
		t.Fatal("process did not start")
	}
	secondCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = r2.Execute(secondCtx, "bash_run", `{"command":"echo second"}`)
	if err == nil || !strings.Contains(err.Error(), "workspace_busy") {
		t.Fatalf("second process err=%v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("first process did not exit")
	}
	if _, err = r2.Execute(context.Background(), "bash_run", `{"command":"echo after"}`); err != nil {
		t.Fatal(err)
	}
}
