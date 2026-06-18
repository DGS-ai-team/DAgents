package hitl

import (
	"strings"
	"testing"
)

func TestFormatApprovalPrompt(t *testing.T) {
	line := FormatApprovalPrompt(map[string]any{
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": "call-1", "name": "bash_run", "raw_arguments": `{"command":"ls"}`},
			},
		},
	})
	for _, part := range []string{"bash(", "call-1", "待审批"} {
		if !strings.Contains(line, part) {
			t.Fatalf("prompt missing %q: %q", part, line)
		}
	}
}

func TestBuildApprovalResumeSelection(t *testing.T) {
	data := map[string]any{
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": "a", "name": "t1"},
				map[string]any{"id": "b", "name": "t2"},
			},
		},
	}
	rv := BuildApprovalResume(data, true)
	if rv["type"] != "selection" {
		t.Fatalf("type = %v", rv["type"])
	}
	approved, _ := rv["approved"].([]string)
	if len(approved) != 2 {
		t.Fatalf("approved = %v", approved)
	}
}

func TestFormatApprovalInteractiveShowsArgs(t *testing.T) {
	line := FormatApprovalInteractive(map[string]any{
		"message": "检测到工具调用，等待用户确认后继续执行。",
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{
					"id":              "call-1",
					"name":            "trigger_create",
					"raw_arguments":   `{"name":"喝水提醒","schedule":{"kind":"once"}}`,
					"approval_reason": "将创建定时触发器: 喝水提醒",
					"risk_level":      "medium",
				},
			},
		},
	}, map[string]bool{}, 0)
	for _, part := range []string{"trigger_create", "喝水提醒", "参数:", "风险: medium", "原因:"} {
		if !strings.Contains(line, part) {
			t.Fatalf("interactive missing %q:\n%s", part, line)
		}
	}
	if strings.Contains(line, "<nil>") {
		t.Fatalf("should not show nil: %q", line)
	}
}

func TestResolveApprovalSelection_defaults_cursor_when_none_checked(t *testing.T) {
	items := []ToolApprovalItem{
		{CallID: "a", Name: "t1"},
		{CallID: "b", Name: "t2"},
	}
	resolved := ResolveApprovalSelection(items, map[string]bool{}, 1)
	if !resolved["b"] {
		t.Fatalf("expected cursor item approved: %+v", resolved)
	}
	if resolved["a"] {
		t.Fatalf("expected non-cursor item rejected: %+v", resolved)
	}
}
