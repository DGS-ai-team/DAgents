package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseToolCallArguments(t *testing.T) {
	cleaned := ParseToolCallArguments(`{"call_purpose":"run","command":"echo hi"}`)
	if strings.Contains(cleaned, "call_purpose") {
		t.Fatalf("parser must remove display-only call_purpose, got %q", cleaned)
	}
	if got := ParseToolCallArguments(`{"path":"a.txt"}`); got != `{"path":"a.txt"}` {
		t.Fatalf("got %q", got)
	}
}

func TestBashRunSync(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "bash_run", `{"command":"echo ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[BASH_RESULT]") || !strings.Contains(out, "ok") {
		t.Fatalf("out = %q", out)
	}
}

func TestBashRunSyncTimeoutFailsWithoutBackgroundJob(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSession(context.Background(), "sess-timeout")
	out, err := reg.Execute(ctx, "bash_run", `{"command":"sleep 3","timeout_seconds":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status=TIMED_OUT") || strings.Contains(out, "job_id=") {
		t.Fatalf("expected synchronous TIMED_OUT without job_id, got %q", out)
	}
	if counts := reg.SessionToolJobCounts("sess-timeout"); counts.Running != 0 {
		t.Fatalf("timed out bash left a job behind: %+v", counts)
	}
}

func TestBashRunOmitTimeoutHardKill(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.bashHardLimitSec = 1
	ctx := WithToolCallID(WithSession(context.Background(), "sess-hard"), "call-hard-1")
	out, err := reg.Execute(ctx, "bash_run", `{"command":"sleep 3"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "status=RUNNING") {
		t.Fatalf("omit timeout must not auto-degrade: %q", out)
	}
	if !strings.Contains(out, "status=TIMED_OUT") || !strings.Contains(out, "timeout_seconds=1") {
		t.Fatalf("expected timed-out result, got %q", out)
	}
}

func TestBashRunCancelSyncViaUI(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithToolCallID(WithSession(context.Background(), "sess-cancel"), "call-cancel-1")
	done := make(chan string, 1)
	go func() {
		out, execErr := reg.Execute(ctx, "bash_run", `{"command":"sleep 20"}`)
		if execErr != nil {
			done <- execErr.Error()
			return
		}
		done <- out
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := reg.CancelSyncBash("sess-cancel", "call-cancel-1"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case out := <-done:
		if !strings.Contains(out, "status=CANCELLED") {
			t.Fatalf("expected CANCELLED, got %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not finish in time")
	}
}

func TestCancelAllSessionJobs(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	start := func(sid, callID string) {
		ctx := WithToolCallID(WithSession(context.Background(), sid), callID)
		go func() { _, _ = reg.Execute(ctx, "bash_run", `{"command":"sleep 30","timeout_seconds":2}`) }()
	}
	start("sess-all", "call-a")
	start("sess-all", "call-b")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c := reg.SessionToolJobCounts("sess-all"); c.Running >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bash calls not registered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := reg.CancelAllSessionJobs("sess-all"); n < 1 {
		t.Fatalf("CancelAllSessionJobs cancelled %d", n)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		if c := reg.SessionToolJobCounts("sess-all"); c.Running == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("bash calls still active after CancelAllSessionJobs")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
