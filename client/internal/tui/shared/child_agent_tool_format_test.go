package shared

import (
	"strings"
	"testing"
)

func TestFormatTemporaryAgentToolTitleWait(t *testing.T) {
	got := FormatTemporaryAgentToolTitle(toolWaitTemporaryAgents, map[string]any{
		"child_session_ids": []any{"child-aaa", "child-bbb"},
		"timeout_seconds":   float64(60),
	})
	if !strings.Contains(got, "2 个临时 Agent") || !strings.Contains(got, "60s") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatWaitTemporaryAgentsResultFriendly(t *testing.T) {
	content := `{"timed_out":false,"results":[{"child_session_id":"child-044064da5881","status":"completed","summary":"# 东莞天气\n\n晴 25°C","turn_count":2,"artifacts":[]},{"child_session_id":"child-e65a8e7cabfc","status":"completed","summary":"# 深圳天气","turn_count":1,"artifacts":[]}]}`
	head, body, ok := formatTemporaryAgentToolResult(toolWaitTemporaryAgents, content, false)
	if !ok {
		t.Fatal("expected handled")
	}
	if !strings.Contains(head, "2/2") || !strings.Contains(head, "wait_temporary_agents") {
		t.Fatalf("head = %q", head)
	}
	if !strings.Contains(body, "child-044064da58") || !strings.Contains(body, "东莞天气") {
		t.Fatalf("body = %q", body)
	}
	if strings.Contains(body, `\n`) {
		t.Fatalf("body should not contain escaped newlines: %q", body)
	}
}

func TestFormatTemporaryAgentStatusArrayResult(t *testing.T) {
	content := `[{"child_session_id":"child-abc","status":"active","summary":"","turn_count":0,"artifacts":[]}]`
	head, body, ok := formatTemporaryAgentToolResult(toolTemporaryAgentStatus, content, false)
	if !ok {
		t.Fatal("expected handled")
	}
	if !strings.Contains(head, "temporary_agent_status") {
		t.Fatalf("head = %q", head)
	}
	if !strings.Contains(body, "child-abc") {
		t.Fatalf("body = %q", body)
	}
}

func TestFormatToolResultWaitTemporaryAgents(t *testing.T) {
	content := `{"timed_out":false,"results":[{"child_session_id":"child-abc","status":"completed","summary":"done","turn_count":1,"artifacts":[]}]}`
	lines := FormatToolEvent("tool_result", map[string]any{
		"tool_name": toolWaitTemporaryAgents,
		"content":   content,
	}, false)
	if len(lines) < 2 {
		t.Fatalf("lines = %v", lines)
	}
	if strings.Contains(lines[1], "{") {
		t.Fatalf("should not dump raw json: %v", lines)
	}
}
