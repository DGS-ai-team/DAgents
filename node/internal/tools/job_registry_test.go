package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseToolCallArguments(t *testing.T) {
	bg, cleaned := ParseToolCallArguments(`{"command":"echo hi","run_in_background":true}`)
	if !bg || strings.Contains(cleaned, "run_in_background") {
		t.Fatalf("bg=%v cleaned=%q", bg, cleaned)
	}
	bg2, _ := ParseToolCallArguments(`{"path":"a.txt"}`)
	if bg2 {
		t.Fatal("expected false by default")
	}
}

func TestBashRunSync(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
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

func TestBashRunBackgroundIsRejected(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.StartBackground(WithSession(context.Background(), "sess-bg"), "sess-bg", "bash_run", "call-1", `{"command":"echo done"}`)
	if err != ErrBackgroundUnsupported {
		t.Fatalf("err=%v, want ErrBackgroundUnsupported", err)
	}
}

func TestBashRunSyncTimeoutFailsWithoutBackgroundJob(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
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
	counts := reg.SessionToolJobCounts("sess-timeout")
	if counts.Running != 0 || counts.Background != 0 {
		t.Fatalf("timed out bash left a job behind: %+v", counts)
	}
}

func TestFormatBackgroundJobAck_mentionsAutoBackfill(t *testing.T) {
	out := formatBackgroundJobAck(&backgroundJob{id: "xyz", toolName: "browser_task"})
	for _, sub := range []string{
		"[TOOL_BACKGROUND]",
		"async_tool_result",
		"通常无需轮询 background_job_status",
	} {
		if !strings.Contains(out, sub) {
			t.Fatalf("missing %q in %q", sub, out)
		}
	}
}

func TestBashRunOmitTimeoutHardKill(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
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
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.bashHardLimitSec = 30
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
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.bashHardLimitSec = 30
	start := func(sid, callID string) {
		ctx := WithToolCallID(WithSession(context.Background(), sid), callID)
		go func() {
			_, _ = reg.Execute(ctx, "bash_run", `{"command":"sleep 30","timeout_seconds":2}`)
		}()
	}
	start("sess-all", "call-a")
	start("sess-all", "call-b")
	deadline := time.Now().Add(5 * time.Second)
	for {
		c := reg.SessionToolJobCounts("sess-all")
		if c.Running+c.Background >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("jobs not registered: %+v", c)
		}
		time.Sleep(20 * time.Millisecond)
	}
	n := reg.CancelAllSessionJobs("sess-all")
	if n < 1 {
		t.Fatalf("CancelAllSessionJobs cancelled %d", n)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		c := reg.SessionToolJobCounts("sess-all")
		if c.Running == 0 && c.Background == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("jobs still active after CancelAll: %+v", c)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBashTimeoutDoesNotNotifyBackgroundCompletion(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan BackgroundJobDone, 1)
	reg.SetBackgroundJobNotifier(func(sessionID string, done BackgroundJobDone) {
		if sessionID != "sess-notify" {
			t.Errorf("sessionID=%q", sessionID)
		}
		got <- done
	})
	ctx := WithToolCallID(WithSession(context.Background(), "sess-notify"), "call-notify-1")
	out, err := reg.Execute(ctx, "bash_run", `{"command":"sleep 3","timeout_seconds":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status=TIMED_OUT") {
		t.Fatalf("expected TIMED_OUT, got %q", out)
	}
	select {
	case done := <-got:
		t.Fatalf("unexpected background completion: %+v", done)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestNotifyJobDoneIdempotent(t *testing.T) {
	reg := &backgroundJobRegistry{jobs: make(map[string]*backgroundJob)}
	var n int
	reg.onDone = func(_ string, _ BackgroundJobDone) { n++ }
	job := &backgroundJob{
		id:         "j1",
		sessionID:  "s1",
		toolName:   "browser_task",
		status:     "succeeded",
		result:     "ok",
		finishedAt: nowMs(),
	}
	reg.notifyJobDone(job)
	reg.notifyJobDone(job)
	reg.notifyJobDone(job)
	if n != 1 {
		t.Fatalf("notify count=%d want 1", n)
	}
}

func TestBackgroundJobTerminalStateCannotBeOverwritten(t *testing.T) {
	job := &backgroundJob{status: jobStatusRunning}

	job.mu.Lock()
	if !job.transitionStatusLocked(jobStatusCancelled, "cancelled") {
		job.mu.Unlock()
		t.Fatal("expected running job to transition to cancelled")
	}
	if job.transitionStatusLocked(jobStatusSucceeded, "late success") {
		job.mu.Unlock()
		t.Fatal("terminal job accepted a late success transition")
	}
	status := job.status
	result := job.result
	job.mu.Unlock()

	if status != jobStatusCancelled {
		t.Fatalf("status=%q want cancelled", status)
	}
	if result != "cancelled" {
		t.Fatalf("result=%q want cancelled", result)
	}
}
