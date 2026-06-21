package full

import (
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func TestOnStreamEventHITLPauseDoneFinishesTurn(t *testing.T) {
	m := &model{
		transcript: tuishared.NewTranscript(0),
		toolFold:   &tuishared.ToolFold{},
		children:   newChildAgentTracker(),
		turn:       tuishared.NewTurnGate(),
	}
	m.turn.NoteSeq(1)
	m.turn.BeginSubmit()

	m.onStreamEvent(nodeapi.StreamEvent{Type: "assistant", Seq: 2, Data: map[string]any{
		"content": "plan",
	}})
	m.onStreamEvent(nodeapi.StreamEvent{Type: "approval_required", Seq: 3, Data: map[string]any{
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": "call_1", "name": "read_file"},
			},
		},
	}})
	m.onStreamEvent(nodeapi.StreamEvent{Type: "done", Seq: 4, Data: map[string]any{
		"finish_reason":  "awaiting_tool_approval",
		"turn_complete":  false,
		"awaiting":       "tool_approval",
	}})

	if m.turn.Awaiting() {
		t.Fatal("HITL pause done should finish turn gate")
	}
	if len(m.hitlQueue) != 1 {
		t.Fatalf("hitl queue len = %d", len(m.hitlQueue))
	}
}

func TestOnStreamEventIgnoresStaleDoneBeforeTurnContent(t *testing.T) {
	m := &model{
		transcript: tuishared.NewTranscript(0),
		toolFold:   &tuishared.ToolFold{},
		turn:       tuishared.NewTurnGate(),
	}
	m.turn.NoteSeq(10)
	m.turn.BeginSubmit()

	m.onStreamEvent(nodeapi.StreamEvent{Type: "done", Seq: 10, Data: map[string]any{}})
	if !m.turn.Awaiting() {
		t.Fatal("stale done should not finish turn")
	}

	m.onStreamEvent(nodeapi.StreamEvent{Type: "assistant", Seq: 11, Data: map[string]any{
		"content": "reply",
	}})
	m.onStreamEvent(nodeapi.StreamEvent{Type: "done", Seq: 12, Data: map[string]any{
		"finish_reason": "stop",
		"turn_complete": true,
	}})
	if m.turn.Awaiting() {
		t.Fatal("turn should finish after content + done")
	}
}

func TestOnStreamEventSideEffectTurnStartBeginsImplicitTurn(t *testing.T) {
	m := &model{
		transcript: tuishared.NewTranscript(0),
		toolFold:   &tuishared.ToolFold{},
		turn:       tuishared.NewTurnGate(),
	}
	m.turn.NoteSeq(1)

	m.onStreamEvent(nodeapi.StreamEvent{
		Type: "side_effect_turn_start",
		Seq:  2,
		Data: map[string]any{
			"source":              "cancel_recovery",
			"side_effect_pending": float64(1),
			"implicit_turn":       true,
		},
	})
	if !m.turn.Awaiting() {
		t.Fatal("side_effect_turn_start should begin implicit turn")
	}

	m.onStreamEvent(nodeapi.StreamEvent{Type: "assistant", Seq: 3, Data: map[string]any{
		"content": "handled",
	}})
	m.onStreamEvent(nodeapi.StreamEvent{Type: "done", Seq: 4, Data: map[string]any{
		"finish_reason": "stop",
		"turn_complete": true,
	}})
	if m.turn.Awaiting() {
		t.Fatal("implicit turn should finish on done after content")
	}
}
