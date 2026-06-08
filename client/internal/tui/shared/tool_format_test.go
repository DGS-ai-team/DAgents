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

func TestFormatToolResultVerboseNoTrailingBlankLine(t *testing.T) {
	lines := FormatToolEvent("tool_result", map[string]any{
		"tool_name": "read_file",
		"content":   "line1\nline2\n",
	}, true)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("blank line in output: %v", lines)
		}
	}
}

func TestToolDisplayNameTrigger(t *testing.T) {
	got := ToolDisplayName("trigger_create", map[string]any{"name": "喝水提醒"})
	if got != "trigger_create(喝水提醒)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatToolCallSkipsUserInformation(t *testing.T) {
	lines := FormatToolEvent("tool_call", map[string]any{
		"tool_calls": []any{
			map[string]any{
				"id":   "call-ask",
				"name": UserInformationToolName,
				"arguments": map[string]any{
					"question": "请选择语言",
					"options":  []any{map[string]any{"id": "go", "label": "Go"}},
				},
			},
		},
	}, false)
	if len(lines) != 0 {
		t.Fatalf("expected skip, lines=%v", lines)
	}
}

func TestFormatToolResultSearchReplaceFriendly(t *testing.T) {
	content := "成功: 是\n路径: .runtime/scripts_menu.md\n替换次数: 1\n匹配行: 5\n---\n--- a/.runtime/scripts_menu.md\n+++ b/.runtime/scripts_menu.md\n+new line\n"
	lines := FormatToolEvent("tool_result", map[string]any{
		"tool_name": "search_replace",
		"content":   content,
	}, false)
	if len(lines) < 2 {
		t.Fatalf("lines=%v", lines)
	}
	if !strings.Contains(lines[0], "1 处替换") || !strings.Contains(lines[0], "scripts_menu") {
		t.Fatalf("head=%q", lines[0])
	}
	preview := strings.Join(lines[1:], "\n")
	if !strings.Contains(preview, "--- a/") && !strings.Contains(preview, "+new line") {
		t.Fatalf("preview=%q", preview)
	}
}

func TestToolDisplayNameUserInformation(t *testing.T) {
	got := ToolDisplayName(UserInformationToolName, map[string]any{"question": "long question"})
	if got != "Agent 询问" {
		t.Fatalf("got %q", got)
	}
}
