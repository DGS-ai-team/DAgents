package hitl

import "testing"

func TestBuildApprovalResumeChildRouting(t *testing.T) {
	data := map[string]any{
		"approval_id":      "appr-1",
		"child_session_id": "child-abc",
		"hitl_scope":       "child_agent",
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": "call-1", "name": "bash_run"},
			},
		},
	}
	rv := BuildApprovalResume(data, true)
	if rv["child_session_id"] != "child-abc" {
		t.Fatalf("child_session_id = %v", rv["child_session_id"])
	}
	if rv["approval_id"] != "appr-1" {
		t.Fatalf("approval_id = %v", rv["approval_id"])
	}
}

func TestIsChildRuntimeEvent(t *testing.T) {
	if !IsChildRuntimeEvent(map[string]any{"child_session_id": "child-x"}) {
		t.Fatal("expected child runtime")
	}
	if IsChildRuntimeEvent(map[string]any{"content": "hi"}) {
		t.Fatal("expected parent runtime")
	}
}
