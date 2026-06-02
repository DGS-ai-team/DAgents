package shared

import (
	"strings"
	"testing"
)

func TestFormatToolCallEventNested(t *testing.T) {
	lines := FormatToolEvent("tool_call", map[string]any{
		"tool_calls": []any{
			map[string]any{
				"id": "call-1",
				"function": map[string]any{
					"name":      "bash_run",
					"arguments": `{"command":"curl wttr.in/Dongguan"}`,
				},
			},
		},
	}, false)
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	for _, part := range []string{"▶ 调用", "bash(", "curl"} {
		if !strings.Contains(lines[0], part) {
			t.Fatalf("line missing %q: %q", part, lines[0])
		}
	}
}

func TestFormatToolResultBashFriendly(t *testing.T) {
	content := "[BASH_RESULT] shell_type=bash status=OK exit_code=0\ncwd=\"/tmp\"\n--- STDOUT ---\nWeather: Sunny\nLine2\n--- STDERR ---\n"
	lines := FormatToolEvent("tool_result", map[string]any{
		"tool_name": "bash_run",
		"content":   content,
	}, false)
	if len(lines) < 2 {
		t.Fatalf("lines = %v", lines)
	}
	if !strings.Contains(lines[0], "✓") || !strings.Contains(lines[0], "exit=0") {
		t.Fatalf("head = %q", lines[0])
	}
	if !strings.Contains(lines[1], "Weather: Sunny") {
		t.Fatalf("preview = %q", lines[1])
	}
	if strings.Contains(lines[1], "cwd=") {
		t.Fatalf("should hide cwd noise: %q", lines[1])
	}
}

func TestToolDisplayNameTrigger(t *testing.T) {
	got := ToolDisplayName("trigger_create", map[string]any{"name": "喝水提醒"})
	if got != "trigger_create(喝水提醒)" {
		t.Fatalf("got %q", got)
	}
}
