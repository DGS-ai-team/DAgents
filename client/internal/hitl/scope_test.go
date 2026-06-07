package hitl

import "testing"

func TestBuildApprovalResumeChildRouting(t *testing.T) {
	data := map[string]any{
		"approval_id":      "appr-1",
		"child_session_id": "child-abc",
		"hitl_scope":       "temporary_agent",
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

func TestApprovalQueueKey(t *testing.T) {
	if got := ApprovalQueueKey(map[string]any{"child_session_id": "child-a"}); got != "child:child-a" {
		t.Fatalf("child = %q", got)
	}
	if got := ApprovalQueueKey(map[string]any{"approval_id": "appr-1"}); got != "parent:appr-1" {
		t.Fatalf("parent = %q", got)
	}
	if got := ApprovalQueueKey(map[string]any{}); got != "parent:" {
		t.Fatalf("default = %q", got)
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
