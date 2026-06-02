package full

import (
	"testing"

	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
)

func TestChildAgentTrackerLifecycle(t *testing.T) {
	tr := newChildAgentTracker()
	tr.onCreated(map[string]any{
		"child_session_id": "child-1",
		"purpose":          "demo",
	})
	active, pending := tr.counts()
	if active != 1 || pending != 0 {
		t.Fatalf("active=%d pending=%d", active, pending)
	}
	tr.setAwaitingApproval("child-1", true)
	_, pending = tr.counts()
	if pending != 1 {
		t.Fatalf("pending = %d", pending)
	}
	tr.onFinished("child-1")
	active, pending = tr.counts()
	if active != 0 || pending != 0 {
		t.Fatalf("after finish active=%d pending=%d", active, pending)
	}
}

func TestFormatChildLifecycleLine(t *testing.T) {
	line := clihitl.FormatChildLifecycleLine("child_agent_created", map[string]any{
		"child_session_id": "child-abcdef123456",
		"purpose":          "review",
	})
	if line == "" {
		t.Fatal("empty line")
	}
}
