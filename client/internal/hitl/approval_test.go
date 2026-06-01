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
	for _, part := range []string{"bash_run", "call-1", "待审批"} {
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
