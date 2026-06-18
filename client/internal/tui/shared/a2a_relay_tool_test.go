package shared

import "testing"

func TestFormatA2ARelayApprovalPending(t *testing.T) {
	lines := FormatA2ARelayApprovalPending("call-1", "bash(echo hi)", "Node-A", `{"command":"echo hi"}`)
	if len(lines) < 2 {
		t.Fatalf("lines=%v", lines)
	}
	if !IsToolA2APendingLine(lines[0]) {
		t.Fatalf("pending line=%q", lines[0])
	}
	if got := ToolA2ALineBody(lines[0], toolA2APendingLinePrefix); got != "▶ bash(echo hi) from Node-A · 待审批" {
		t.Fatalf("body=%q", got)
	}
}

func TestFormatA2ARelayToolResult(t *testing.T) {
	lines := FormatA2ARelayToolResult("call-1", "bash(date)", "合规助手", true)
	if len(lines) != 2 {
		t.Fatalf("lines=%v", lines)
	}
	if !IsToolA2AResultLine(lines[0]) {
		t.Fatalf("result line=%q", lines[0])
	}
	body := ToolA2ALineBody(lines[0], toolA2AResultLinePrefix)
	if body != "bash(date) from 合规助手" {
		t.Fatalf("body=%q", body)
	}
	preview := ToolA2ALineBody(lines[1], toolPreviewLinePrefix)
	if preview != "已审批，由合规助手执行" {
		t.Fatalf("preview=%q", preview)
	}
}

func TestReplaceA2ARelayToolLines(t *testing.T) {
	tr := NewTranscript(0)
	pending := FormatA2ARelayApprovalPending("call-1", "bash", "peer", "")
	for _, line := range pending {
		tr.Add(line)
	}
	result := FormatA2ARelayToolResult("call-1", "bash", "peer", true)
	tr.ReplaceA2ARelayToolLines("call-1", result)
	raw := tr.Lines()
	if len(raw) != 2 {
		t.Fatalf("lines=%v", raw)
	}
	if !IsToolA2AResultLine(raw[0]) {
		t.Fatalf("expected result line, got %q", raw[0])
	}
}

func TestDisplayA2APendingLine(t *testing.T) {
	line := formatToolMetaLine(toolA2APendingLinePrefix, "c1", "▶ bash from peer · 待审批")
	out := FormatTranscriptLineForDisplay(line, 120)
	if out == "" || out == line {
		t.Fatalf("display=%q", out)
	}
}
