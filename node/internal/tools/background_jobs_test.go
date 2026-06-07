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
