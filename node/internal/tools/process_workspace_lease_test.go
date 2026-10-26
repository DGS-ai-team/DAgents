package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func TestBashRunTimeoutIncludesWorkspaceLeaseWait(t *testing.T) {
	root := t.TempDir()
	r, err := NewRegistry(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	c := workspacecoord.New()
	r.SetWorkspaceCoordinator(c)
	lease, err := r.acquireWorkspaceWrite(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	started := time.Now()
	_, _, err = runShellSync(r, context.Background(), shellRunParams{
		command:    "echo should-not-start",
		cwd:        root,
		shellType:  shellTypeForTest(),
		timeoutSec: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "workspace_busy") {
		t.Fatalf("lease wait err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("lease wait ignored timeout: elapsed=%s", elapsed)
	}
}

func TestBashRunUICancelDuringWorkspaceLeaseWaitIsCancelledWithoutStartingProcess(t *testing.T) {
	root := t.TempDir()
	r, err := NewRegistry(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	c := workspacecoord.New()
	r.SetWorkspaceCoordinator(c)
	lease, err := r.acquireWorkspaceWrite(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	marker := filepath.Join(root, "bash-started.marker")
	command := fmt.Sprintf("printf started > '%s'", marker)
	if runtime.GOOS == "windows" {
		command = fmt.Sprintf("Set-Content -LiteralPath '%s' -Value started", marker)
	}
	ctx := WithToolCallID(WithSession(context.Background(), "lease-cancel"), "lease-cancel-1")
	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, runErr := r.Execute(ctx, "bash_run", fmt.Sprintf(`{"command":%q}`, command))
		done <- struct {
			out string
			err error
		}{out: out, err: runErr}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := r.syncShells.get("lease-cancel-1"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bash_run did not register for UI cancellation")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := r.CancelSyncBash("lease-cancel", "lease-cancel-1"); err != nil {
		t.Fatalf("cancel bash_run while waiting for lease: %v", err)
	}
	select {
	case result := <-done:
		if result.err != nil || !strings.Contains(result.out, "status=CANCELLED") {
			t.Fatalf("expected CANCELLED without error, out=%q err=%v", result.out, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled bash_run did not finish while waiting for lease")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("cancelled command had execution side effects: stat err=%v", err)
	}
}

func shellTypeForTest() shellType {
	if runtime.GOOS == "windows" {
		return shellPowerShell
	}
	return shellBash
}
