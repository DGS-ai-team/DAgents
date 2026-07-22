package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseRunInBackground(t *testing.T) {
	bg, cleaned := ParseRunInBackground(`{"command":"echo hi","run_in_background":true}`)
	if !bg || strings.Contains(cleaned, "run_in_background") {
		t.Fatalf("bg=%v cleaned=%q", bg, cleaned)
	}
	bg2, _ := ParseRunInBackground(`{"path":"a.txt"}`)
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

func TestBashRunBackgroundViaStartBackground(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSession(context.Background(), "sess-bg")
	ack, err := reg.StartBackground(ctx, "sess-bg", "bash_run", "call-1", `{"command":"echo done"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ack, "[TOOL_BACKGROUND]") || !strings.Contains(ack, "job_id=") {
		t.Fatalf("ack = %q", ack)
	}
	jobID := extractField(ack, "job_id=")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := reg.Execute(context.Background(), "background_job_status", `{"job_id":"`+jobID+`"}`)
		if strings.Contains(status, "status=succeeded") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("background bash did not succeed in time")
}

func TestBashRunSyncTimeoutAutoDegrade(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSession(context.Background(), "sess-degrade")
	out, err := reg.Execute(ctx, "bash_run", `{"command":"sleep 3","timeout_seconds":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status=RUNNING") || !strings.Contains(out, "job_id=") {
		t.Fatalf("expected RUNNING degrade, got %q", out)
	}
	jobID := extractField(out, "job_id=")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := reg.Execute(context.Background(), "background_job_status", `{"job_id":"`+jobID+`"}`)
		if strings.Contains(status, "status=succeeded") || strings.Contains(status, "status=failed") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("auto-degraded bash job did not finish in time")
}

func TestBackgroundJobCancel(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ack, err := reg.StartBackground(context.Background(), "sess-x", "bash_run", "call-2", `{"command":"sleep 5"}`)
	if err != nil {
		t.Fatal(err)
	}
	jobID := extractField(ack, "job_id=")
	cancel, err := reg.Execute(context.Background(), "background_job_cancel", `{"job_id":"`+jobID+`"}`)
	if err != nil || !strings.Contains(cancel, "cancelled") {
		t.Fatalf("cancel = %q err=%v", cancel, err)
	}
}

func extractField(text, prefix string) string {
	idx := strings.Index(text, prefix)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(prefix):]
	end := strings.IndexAny(rest, " \n")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return rest[:end]
}

func TestFormatShellRunningResult_mentionsAutoBackfill(t *testing.T) {
	job := &backgroundJob{
		id:            "abc123",
		bashShellType: "bash",
	}
	out := formatShellRunningResult(job, shellRunParams{shellType: shellBash}, "timeout")
	for _, sub := range []string{
		"[BASH_RESULT] status=RUNNING job_id=abc123",
		"async_tool_result",
		"通常无需轮询 background_job_status",
		"已自动降级为后台任务",
	} {
		if !strings.Contains(out, sub) {
			t.Fatalf("missing %q in %q", sub, out)
		}
	}
	if strings.Contains(out, "可用 background_job_status / background_job_cancel 查询或取消") {
		t.Fatalf("old poll-first wording should be gone: %q", out)
	}
	userOut := formatShellRunningResult(job, shellRunParams{shellType: shellBash}, "user")
	if !strings.Contains(userOut, "已按用户请求转为后台任务") {
		t.Fatalf("user reason missing: %q", userOut)
	}
}

func TestFormatBackgroundJobAck_mentionsAutoBackfill(t *testing.T) {
	out := formatBackgroundJobAck(&backgroundJob{id: "xyz", toolName: "bash_run"})
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

func TestBackgroundJobHints_alignedBetweenDegradeAndACK(t *testing.T) {
	degrade := formatShellRunningResult(&backgroundJob{id: "j1", bashShellType: "bash"}, shellRunParams{}, "timeout")
	ack := formatBackgroundJobAck(&backgroundJob{id: "j2", toolName: "bash_run"})
	if !strings.Contains(degrade, backgroundJobAutoResultHint) || !strings.Contains(ack, backgroundJobAutoResultHint) {
		t.Fatal("auto result hint must match")
	}
	if !strings.Contains(degrade, backgroundJobOptionalMgmtHint) || !strings.Contains(ack, backgroundJobOptionalMgmtHint) {
		t.Fatal("optional mgmt hint must match")
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
	if !strings.Contains(out, "status=ERROR") || !strings.Contains(out, "硬上限") {
		t.Fatalf("expected hard-limit ERROR, got %q", out)
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

func TestBashRunBackgroundSyncViaUI(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.bashHardLimitSec = 30
	ctx := WithToolCallID(WithSession(context.Background(), "sess-bg-ui"), "call-bg-1")
	done := make(chan string, 1)
	go func() {
		out, execErr := reg.Execute(ctx, "bash_run", `{"command":"sleep 3"}`)
		if execErr != nil {
			done <- execErr.Error()
			return
		}
		done <- out
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := reg.BackgroundSyncBash("sess-bg-ui", "call-bg-1"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case out := <-done:
		if !strings.Contains(out, "status=RUNNING") || !strings.Contains(out, "job_id=") {
			t.Fatalf("expected RUNNING after UI background, got %q", out)
		}
		if !strings.Contains(out, "已按用户请求转为后台任务") {
			t.Fatalf("expected user background wording, got %q", out)
		}
		counts := reg.SessionToolJobCounts("sess-bg-ui")
		if counts.Running != 0 {
			t.Fatalf("running should be 0 after background, got %+v", counts)
		}
		if counts.Background < 1 {
			t.Fatalf("background should be >=1, got %+v", counts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background did not finish in time")
	}
}
