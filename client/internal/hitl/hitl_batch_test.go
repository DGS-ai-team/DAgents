package hitl

import "testing"

func TestExpandHITLRequiredChildRouting(t *testing.T) {
	data := map[string]any{
		"hitl_id":          "hitl-child-1",
		"child_session_id": "child-abc",
		"hitl_scope":       "temporary_agent",
		"child_purpose":    "research",
		"items": []any{
			map[string]any{
				"hitl_type": HITLTypeExecuteTool,
				"id":        "call-bash-1",
				"name":      "bash_run",
				"arguments": map[string]any{"command": "echo ok"},
			},
		},
	}
	_, approval := ExpandHITLRequired(data)
	if approval == nil {
		t.Fatal("expected approval data")
	}
	if got := ChildSessionIDFromData(approval); got != "child-abc" {
		t.Fatalf("child_session_id = %q", got)
	}
	rv := BuildApprovalResume(approval, true)
	if rv["child_session_id"] != "child-abc" {
		t.Fatalf("resume child_session_id = %v", rv["child_session_id"])
	}
}

func TestExpandHITLRequiredMixedBatch(t *testing.T) {
	data := map[string]any{
		"hitl_id": "hitl-1",
		"message": "mixed",
		"items": []any{
			map[string]any{
				"hitl_type": HITLTypeUserInformation,
				"content":   "Pick one?",
				"user_information_args": map[string]any{
					"tool_call_id": "call-ask-1",
					"question":     "Pick one?",
				},
			},
			map[string]any{
				"hitl_type": HITLTypeExecuteTool,
				"id":        "call-bash-1",
				"name":      "bash_run",
				"arguments": map[string]any{"command": "echo ok"},
			},
		},
	}
	userInfos, approval := ExpandHITLRequired(data)
	if len(userInfos) != 1 {
		t.Fatalf("userInfos = %d, want 1", len(userInfos))
	}
	if approval == nil {
		t.Fatal("expected approval data")
	}
	items := ExtractToolApprovals(approval)
	if len(items) != 1 || items[0].CallID != "call-bash-1" {
		t.Fatalf("approval items = %+v", items)
	}
	req := ExtractUserInformationRequest(userInfos[0])
	if req == nil || req.ToolCallID != "call-ask-1" {
		t.Fatalf("user info req = %+v", req)
	}
}
