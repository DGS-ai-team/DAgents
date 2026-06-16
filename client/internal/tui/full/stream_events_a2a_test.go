package full

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func TestOnStreamEventA2ARelayApprovalReleasesTurnWait(t *testing.T) {
	m := &model{
		transcript: tuishared.NewTranscript(0),
		toolFold:   &tuishared.ToolFold{},
		children:   newChildAgentTracker(),
		turn:       tuishared.NewTurnGate(),
	}
	m.turn.NoteSeq(1)
	m.turn.BeginSubmit()
	m.onStreamEvent(nodeapi.StreamEvent{
		Type: "approval_required",
		Seq:  2,
		Data: map[string]any{
			"a2a_relay":   true,
			"a2a_task_id": "task-1",
			"approval_args": map[string]any{
				"tool_calls": []any{
					map[string]any{"id": "call_1", "name": "bash_run"},
				},
			},
		},
	})
	if m.turn.Awaiting() {
		t.Fatal("expected turn gate released for a2a relay approval")
	}
	if len(m.hitlQueue) != 1 {
		t.Fatalf("hitl queue len = %d", len(m.hitlQueue))
	}
}

func TestOnStreamEventA2ARelayApprovalPeerMeta(t *testing.T) {
	m := &model{
		transcript:     tuishared.NewTranscript(0),
		toolFold:       &tuishared.ToolFold{},
		toolBlocks:     tuishared.NewToolBlockRegistry(),
		toolPending:    tuishared.NewToolPendingTracker(),
		toolCallStream: tuishared.NewToolCallStreamState(),
		children:       newChildAgentTracker(),
		turn:           tuishared.NewTurnGate(),
	}
	m.onStreamEvent(nodeapi.StreamEvent{
		Type: "approval_required",
		Seq:  2,
		Data: map[string]any{
			"a2a_relay":           true,
			"a2a_peer_agent_name": "合规助手",
			"approval_args": map[string]any{
				"tool_calls": []any{
					map[string]any{"id": "call_1", "name": "bash_run"},
				},
			},
		},
	})
	m.showNextHITLIfIdle()
	if m.mode != modeApproval {
		t.Fatalf("mode=%v", m.mode)
	}
	if len(m.transcript.Lines()) == 0 {
		t.Fatal("expected a2a pending block after showNextHITLIfIdle")
	}
}

func TestOnStreamEventA2ARelayUserInfoReleasesTurnWait(t *testing.T) {
	m := &model{
		transcript:     tuishared.NewTranscript(0),
		toolFold:       &tuishared.ToolFold{},
		toolBlocks:     tuishared.NewToolBlockRegistry(),
		toolPending:    tuishared.NewToolPendingTracker(),
		toolCallStream: tuishared.NewToolCallStreamState(),
		children:       newChildAgentTracker(),
		turn:           tuishared.NewTurnGate(),
		input:          textarea.New(),
	}
	m.turn.NoteSeq(1)
	m.turn.BeginSubmit()
	m.onStreamEvent(nodeapi.StreamEvent{
		Type: "user_information_required",
		Seq:  2,
		Data: map[string]any{
			"a2a_relay":           true,
			"a2a_peer_agent_name": "合规助手",
			"user_information_args": map[string]any{
				"tool_call_id": "call-ask-1",
				"question":     "请确认环境",
			},
		},
	})
	if m.turn.Awaiting() {
		t.Fatal("expected turn gate released for a2a relay user_information")
	}
	if len(m.hitlQueue) != 1 {
		t.Fatalf("hitl queue len = %d", len(m.hitlQueue))
	}
	m.showNextHITLIfIdle()
	if m.mode != modeUserInfo {
		t.Fatalf("mode=%v", m.mode)
	}
	if !strings.Contains(m.statusLine, "A2A") {
		t.Fatalf("status=%q", m.statusLine)
	}
}
