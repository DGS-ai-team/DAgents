package full

import (
	"strings"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func TestOnStreamEventFiltersChildAssistant(t *testing.T) {
	m := &model{
		transcript: tuishared.NewTranscript(0),
		toolFold:   &tuishared.ToolFold{},
		children:   newChildAgentTracker(),
		turn:       tuishared.NewTurnGate(),
	}
	m.onStreamEvent(nodeapi.StreamEvent{Type: "assistant", Seq: 1, Data: map[string]any{
		"child_session_id": "child-1",
		"content":          "secret child text",
	}})
	if m.transcript.Len() != 0 {
		t.Fatalf("child assistant leaked, lines=%d", m.transcript.Len())
	}

	m.turn.BeginSubmit()
	m.onStreamEvent(nodeapi.StreamEvent{Type: "assistant", Seq: 2, Data: map[string]any{
		"content": "parent hello",
	}})
	m.onStreamEvent(nodeapi.StreamEvent{Type: "done", Seq: 3, Data: map[string]any{}})
	lines := m.transcript.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], "parent hello") {
		t.Fatalf("parent line missing: %v", lines)
	}

	m.onStreamEvent(nodeapi.StreamEvent{Type: "approval_required", Data: map[string]any{
		"child_session_id": "child-2",
		"hitl_scope":       "temporary_agent",
		"child_purpose":    "ops",
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": "c1", "name": "bash_run"},
			},
		},
	}})
	if len(m.hitlQueue) != 1 {
		t.Fatalf("approval queue len = %d", len(m.hitlQueue))
	}
	if !clihitl.IsTemporaryAgentApproval(m.hitlQueue[0].data) {
		t.Fatal("expected child approval in queue")
	}
}
