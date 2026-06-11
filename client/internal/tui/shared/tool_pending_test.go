package shared

import (
	"strings"
	"testing"
	"time"
)

func TestToolPendingTracker_elapsedFormat(t *testing.T) {
	tr := NewToolPendingTracker()
	tr.Register("call-1", "调用 bash(echo)")
	line := formatToolMetaLine(toolPendingLinePrefix, "call-1", "▶ 调用 bash(echo)")
	got := tr.FormatPendingLine(line)
	if !strings.Contains(got, "bash") {
		t.Fatalf("got %q", got)
	}
	time.Sleep(1100 * time.Millisecond)
	got2 := tr.FormatPendingLine(line)
	if !strings.Contains(got2, "1s") {
		t.Fatalf("expected elapsed, got %q", got2)
	}
}
