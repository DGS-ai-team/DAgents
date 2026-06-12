package shared

import (
	"strings"
	"testing"
	"time"
)

func TestToolPendingTracker_elapsedFormat(t *testing.T) {
	tr := NewToolPendingTracker()
	tr.Register("call-1", "bash(echo)")
	line := formatToolMetaLine(toolPendingLinePrefix, "call-1", "▶ 调用 bash(echo)")
	got := tr.FormatPendingLine(line)
	if !strings.Contains(got, "bash") || !strings.Contains(got, "▶") {
		t.Fatalf("got %q", got)
	}
	time.Sleep(1100 * time.Millisecond)
	got2 := tr.FormatPendingLine(line)
	if !strings.Contains(got2, "1s") {
		t.Fatalf("expected elapsed, got %q", got2)
	}
}

func TestFormatToolElapsed(t *testing.T) {
	if got := FormatToolElapsed(0.42); got != "420ms" {
		t.Fatalf("got %q", got)
	}
	if got := FormatToolElapsed(2.3); got != "2.3s" {
		t.Fatalf("got %q", got)
	}
}

func TestToolDisplayNameAgentInvoke(t *testing.T) {
	got := ToolDisplayName("agent_invoke", map[string]any{
		CallPurposeKey: "合规咨询",
		"to_agent_id":  "node-b",
	})
	if got != "agent_invoke(合规咨询)" {
		t.Fatalf("got %q", got)
	}
	got2 := ToolDisplayName("agent_invoke", map[string]any{"to_agent_id": "node-b"})
	if got2 != `agent_invoke(to_agent_id="node-b")` {
		t.Fatalf("got %q", got2)
	}
}

func TestToolDisplayNameBashCallPurpose(t *testing.T) {
	got := ToolDisplayName("bash_run", map[string]any{
		CallPurposeKey: "检查 HTTP 端口",
		"command":        "curl localhost:8080",
	})
	if got != "bash(检查 HTTP 端口)" {
		t.Fatalf("got %q", got)
	}
}

func TestToolCallPartsBashShowsCommandWhenPurpose(t *testing.T) {
	_, code := ToolCallParts("bash_run", map[string]any{
		CallPurposeKey: "检查端口",
		"command":      "curl example.com",
	})
	if code != "curl example.com" {
		t.Fatalf("code=%q", code)
	}
}

func TestToolCallPartsWriteFileContent(t *testing.T) {
	_, code := ToolCallParts("write_file", map[string]any{
		"path":    "/tmp/a.txt",
		"content": "hello\nworld",
	})
	if code != "hello\nworld" {
		t.Fatalf("code=%q", code)
	}
}

func TestFormatToolResultWithElapsed(t *testing.T) {
	lines := FormatToolEventWithID("tool_result", map[string]any{
		"tool_name": "read_file",
		"content":   "ok",
	}, "call-1", false, 1.2)
	if len(lines) == 0 || !strings.Contains(lines[0], "1.2s") {
		t.Fatalf("lines=%v", lines)
	}
}
