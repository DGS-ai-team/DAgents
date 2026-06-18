package full

import (
	"strings"
	"testing"

	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func a2aRelayApprovalData() map[string]any {
	return map[string]any{
		"a2a_relay":           true,
		"a2a_task_id":         "task-1",
		"a2a_peer_agent_name": "合规助手",
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{
					"id":        "call_1",
					"name":      "bash_run",
					"arguments": map[string]any{"command": "date"},
				},
			},
		},
	}
}

func TestEnsureA2ARelayApprovalToolBlocks(t *testing.T) {
	m := &model{
		transcript:     tuishared.NewTranscript(0),
		toolFold:       &tuishared.ToolFold{},
		toolBlocks:     tuishared.NewToolBlockRegistry(),
		toolPending:    tuishared.NewToolPendingTracker(),
		toolCallStream: tuishared.NewToolCallStreamState(),
	}
	data := a2aRelayApprovalData()
	m.ensureA2ARelayApprovalToolBlocks(data)
	lines := m.transcript.Lines()
	if len(lines) < 1 {
		t.Fatal("expected pending tool block")
	}
	if !tuishared.IsToolA2APendingLine(lines[0]) {
		t.Fatalf("line=%q", lines[0])
	}
	if !strings.Contains(lines[0], "from 合规助手") {
		t.Fatalf("missing peer label: %q", lines[0])
	}
	if m.toolPending.Len() != 0 {
		t.Fatalf("a2a relay should not register yellow pending tracker, len=%d", m.toolPending.Len())
	}
}

func TestFinalizeA2ARelayToolBlocksOnApproval(t *testing.T) {
	m := &model{
		transcript:     tuishared.NewTranscript(0),
		toolFold:       &tuishared.ToolFold{},
		toolBlocks:     tuishared.NewToolBlockRegistry(),
		toolPending:    tuishared.NewToolPendingTracker(),
		toolCallStream: tuishared.NewToolCallStreamState(),
	}
	data := a2aRelayApprovalData()
	m.ensureA2ARelayApprovalToolBlocks(data)
	resume := clihitl.BuildApprovalResume(data, true)
	m.finalizeA2ARelayToolBlocks(data, resume)
	lines := m.transcript.Lines()
	foundResult := false
	for _, line := range lines {
		if tuishared.IsToolA2AResultLine(line) {
			foundResult = true
			if !strings.Contains(line, "from 合规助手") {
				t.Fatalf("result missing peer: %q", line)
			}
		}
		if tuishared.IsToolA2APendingLine(line) {
			t.Fatalf("pending should be replaced: %q", line)
		}
	}
	if !foundResult {
		t.Fatalf("lines=%v", lines)
	}
}

func TestFinalizeA2ARelayToolBlocksOnReject(t *testing.T) {
	m := &model{
		transcript:     tuishared.NewTranscript(0),
		toolFold:       &tuishared.ToolFold{},
		toolBlocks:     tuishared.NewToolBlockRegistry(),
		toolPending:    tuishared.NewToolPendingTracker(),
		toolCallStream: tuishared.NewToolCallStreamState(),
	}
	data := a2aRelayApprovalData()
	m.ensureA2ARelayApprovalToolBlocks(data)
	resume := clihitl.BuildApprovalResume(data, false)
	m.finalizeA2ARelayToolBlocks(data, resume)
	for _, line := range m.transcript.Lines() {
		if tuishared.IsToolA2AResultLine(line) {
			if !strings.Contains(line, "已拒绝") {
				t.Fatalf("expected rejected marker in %q", line)
			}
			return
		}
	}
	t.Fatalf("lines=%v", m.transcript.Lines())
}

func TestInitApprovalStateA2ARelayCreatesBlocks(t *testing.T) {
	m := &model{
		transcript:     tuishared.NewTranscript(0),
		toolFold:       &tuishared.ToolFold{},
		toolBlocks:     tuishared.NewToolBlockRegistry(),
		toolPending:    tuishared.NewToolPendingTracker(),
		toolCallStream: tuishared.NewToolCallStreamState(),
	}
	m.initApprovalState(a2aRelayApprovalData())
	if len(m.transcript.Lines()) == 0 {
		t.Fatal("expected synthetic tool block")
	}
}
