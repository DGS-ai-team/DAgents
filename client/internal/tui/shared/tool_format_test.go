package shared

import (
	"strings"
	"testing"
)

func TestFormatToolCallEventWithCallPurpose(t *testing.T) {
	lines := FormatToolEvent("tool_call", map[string]any{
		"tool_calls": []any{
			map[string]any{
				"id": "call-1",
				"function": map[string]any{
					"name":      "bash_run",
					"arguments": `{"call_purpose":"检查服务端口","command":"curl wttr.in/Dongguan"}`,
				},
			},
		},
	}, false)
	if len(lines) < 2 {
		t.Fatalf("lines = %v", lines)
	}
	for _, part := range []string{"▶ 调用", "bash(检查服务端口)"} {
		if !strings.Contains(lines[0], part) {
			t.Fatalf("line missing %q: %q", part, lines[0])
		}
	}
	if !IsToolCallCodeLine(lines[1]) || !strings.Contains(lines[1], "curl") {
		t.Fatalf("expected command preview line: %v", lines)
	}
}

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
	content := "[BASH_RESULT] exit=0\n--- STDOUT ---\nWeather: Sunny\nLine2\n--- STDERR ---\n"
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
}

func TestFormatToolResultBashCompressPct(t *testing.T) {
	content := "[BASH_RESULT] exit=0\n--- STDOUT ---\nok\n--- STDERR ---\n"
	lines := FormatToolEvent("tool_result", map[string]any{
		"tool_name":                 "bash_run",
		"content":                   content,
		"output_compress_saved_pct": 42,
	}, false)
	if len(lines) == 0 {
		t.Fatal("no lines")
	}
	if !strings.Contains(lines[0], "· -42%") {
		t.Fatalf("head = %q", lines[0])
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
	got := ToolDisplayName("trigger_create", map[string]any{
		CallPurposeKey: "定时提醒喝水",
		"name":         "喝水提醒",
	})
	if got != "trigger_create(定时提醒喝水)" {
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

func TestFormatToolResultSearchReplaceFriendly_minimalSuccess(t *testing.T) {
	content := "成功: 是\n替换次数: 1"
	lines := FormatToolEvent("tool_result", map[string]any{
		"tool_name": "search_replace",
		"content":   content,
	}, false)
	if len(lines) != 1 {
		t.Fatalf("lines=%v", lines)
	}
	if !strings.Contains(lines[0], "1 处替换") {
		t.Fatalf("head=%q", lines[0])
	}
}

func TestFormatToolResultSearchReplaceFriendly_withPreview(t *testing.T) {
	content := "成功: 是\n替换次数: 2\n---\n@@ 共 2 处相同替换 · 行 1、3 @@\n-foo\n+bar\n"
	lines := FormatToolEvent("tool_result", map[string]any{
		"tool_name": "search_replace",
		"content":   content,
	}, false)
	if len(lines) < 2 {
		t.Fatalf("lines=%v", lines)
	}
	if !strings.Contains(lines[0], "2 处替换") {
		t.Fatalf("head=%q", lines[0])
	}
	preview := strings.Join(lines[1:], "\n")
	if !strings.Contains(preview, "+bar") || !strings.Contains(preview, "-foo") {
		t.Fatalf("preview=%q", preview)
	}
}

func TestFormatToolResultAgentDiscover(t *testing.T) {
	content := `{"agents":[{"agent_id":"node-a","name":"Node A"},{"agent_id":"node-b","name":"Node B"}]}`
	lines := FormatToolEvent("tool_result", map[string]any{
		"tool_name": "agent_discover",
		"content":   content,
	}, false)
	if len(lines) < 2 {
		t.Fatalf("lines=%v", lines)
	}
	if !strings.Contains(lines[0], "Node A") && !strings.Contains(lines[0], "2") {
		t.Fatalf("head=%q", lines[0])
	}
}

func TestToolDisplayNameUserInformation(t *testing.T) {
	got := ToolDisplayName(UserInformationToolName, map[string]any{"question": "long question"})
	if got != "Agent 询问" {
		t.Fatalf("got %q", got)
	}
}
