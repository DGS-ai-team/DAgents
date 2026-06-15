package tools

import (
	"strings"
	"testing"
)

func TestFormatShellRunningResult_mentionsAutoBackfill(t *testing.T) {
	job := &backgroundJob{
		id:            "abc123",
		bashShellType: "bash",
	}
	out := formatShellRunningResult(job, shellRunParams{shellType: shellBash})
	for _, sub := range []string{
		"[BASH_RESULT] status=RUNNING job_id=abc123",
		"async_tool_result",
		"通常无需轮询 background_job_status",
	} {
		if !strings.Contains(out, sub) {
			t.Fatalf("missing %q in %q", sub, out)
		}
	}
	if strings.Contains(out, "可用 background_job_status / background_job_cancel 查询或取消") {
		t.Fatalf("old poll-first wording should be gone: %q", out)
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
	degrade := formatShellRunningResult(&backgroundJob{id: "j1", bashShellType: "bash"}, shellRunParams{})
	ack := formatBackgroundJobAck(&backgroundJob{id: "j2", toolName: "bash_run"})
	if !strings.Contains(degrade, backgroundJobAutoResultHint) || !strings.Contains(ack, backgroundJobAutoResultHint) {
		t.Fatal("auto result hint must match")
	}
	if !strings.Contains(degrade, backgroundJobOptionalMgmtHint) || !strings.Contains(ack, backgroundJobOptionalMgmtHint) {
		t.Fatal("optional mgmt hint must match")
	}
}
