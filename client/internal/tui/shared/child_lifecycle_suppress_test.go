package shared

import "testing"

func TestChildLifecycleSuppressAfterWaitToolResult(t *testing.T) {
	s := NewChildLifecycleSuppress()
	s.NoteToolCallEvent(map[string]any{
		"tool_calls": []any{
			map[string]any{
				"id": "call-1",
				"function": map[string]any{
					"name":      toolWaitTemporaryAgents,
					"arguments": `{"child_session_ids":["child-a","child-b"]}`,
				},
			},
		},
	})
	if !s.ShouldSuppressLifecycle("child-a", "temporary_agent_completed") {
		t.Fatal("expected suppress while wait pending")
	}
	content := `{"timed_out":false,"results":[{"child_session_id":"child-a","status":"completed","summary":"done","turn_count":1,"artifacts":[]},{"child_session_id":"child-b","status":"completed","summary":"done2","turn_count":1,"artifacts":[]}]}`
	s.NoteToolResult(toolWaitTemporaryAgents, content)
	if !s.ShouldSuppressLifecycle("child-a", "temporary_agent_completed") {
		t.Fatal("expected suppress after wait tool result")
	}
	if s.ShouldSuppressLifecycle("child-c", "temporary_agent_completed") {
		t.Fatal("unexpected suppress for unrelated child")
	}
}

func TestChildLifecycleSuppressDoesNotHideCreated(t *testing.T) {
	s := NewChildLifecycleSuppress()
	s.NoteToolCallEvent(map[string]any{
		"tool_calls": []any{
			map[string]any{
				"function": map[string]any{
					"name":      toolWaitTemporaryAgents,
					"arguments": `{"child_session_ids":["child-a"]}`,
				},
			},
		},
	})
	if s.ShouldSuppressLifecycle("child-a", "temporary_agent_created") {
		t.Fatal("created lifecycle should not be suppressed")
	}
}
