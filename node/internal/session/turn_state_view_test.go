package session

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestTurnStatePhaseProjection(t *testing.T) {
	tests := []struct {
		name  string
		snap  turn.CoordinatorSnapshot
		phase string
		term  bool
	}{
		{
			name: "queued",
			snap: turn.CoordinatorSnapshot{
				TurnID: "turn-1", TurnStatus: turn.TurnStatusRunning,
				StepStatus: turn.StepStatusCreated, HasActiveTurn: true,
			},
			phase: "queued",
		},
		{
			name: "model generating",
			snap: turn.CoordinatorSnapshot{
				TurnID: "turn-1", TurnStatus: turn.TurnStatusRunning,
				StepStatus: turn.StepStatusRequesting, HasActiveTurn: true,
			},
			phase: "model_generating",
		},
		{
			name: "tool execution",
			snap: turn.CoordinatorSnapshot{
				TurnID: "turn-1", TurnStatus: turn.TurnStatusRunning,
				StepStatus: turn.StepStatusExecutingTools, HasActiveTurn: true,
			},
			phase: "tool_executing",
		},
		{
			name: "tool approval",
			snap: turn.CoordinatorSnapshot{
				TurnID: "turn-1", TurnStatus: turn.TurnStatusWaiting,
				StepStatus: turn.StepStatusWaitingInteraction, InteractionKind: "approval",
				HasActiveTurn: true,
			},
			phase: "tool_waiting",
		},
		{
			name: "user information",
			snap: turn.CoordinatorSnapshot{
				TurnID: "turn-1", TurnStatus: turn.TurnStatusWaiting,
				StepStatus: turn.StepStatusWaitingInteraction, InteractionKind: "user_information",
				HasActiveTurn: true,
			},
			phase: "waiting_user",
		},
		{
			name: "terminal",
			snap: turn.CoordinatorSnapshot{
				TurnID: "turn-1", TurnStatus: turn.TurnStatusCancelled,
				StepStatus: turn.StepStatusCancelled, HasActiveTurn: false,
			},
			phase: "cancelled", term: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := buildTurnStateView(tt.snap, 42)
			if view.Phase != tt.phase || view.Terminal != tt.term {
				t.Fatalf("view = %#v, want phase=%q terminal=%t", view, tt.phase, tt.term)
			}
			if view.Authority != "turn_coordinator" || view.LifecycleSeq != 42 {
				t.Fatalf("view authority/sequence = %#v", view)
			}
		})
	}
}

func TestTurnStateEventDataIncludesSafeLifecycleFields(t *testing.T) {
	snapshot := turn.CoordinatorSnapshot{
		TurnID: "turn-1", StepID: "step-1", Generation: 3,
		TurnStatus: turn.TurnStatusRunning, StepStatus: turn.StepStatusRequesting,
		HasActiveTurn: true,
	}
	data := turnStateEventData(snapshot, 9, turn.CommandModelRequestStarted)
	if data["phase"] != "model_generating" || data["lifecycle_seq"] != uint64(9) {
		t.Fatalf("event data = %#v", data)
	}
	if data["command"] != string(turn.CommandModelRequestStarted) {
		t.Fatalf("event command = %#v", data["command"])
	}
}
