package session

import (
	"reflect"
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
	data := turnStateEventData(snapshot, 9, 12, turn.CommandModelRequestStarted)
	if data["phase"] != "model_generating" || data["lifecycle_seq"] != uint64(9) {
		t.Fatalf("event data = %#v", data)
	}
	if data["history_revision"] != uint64(12) {
		t.Fatalf("history revision = %#v", data["history_revision"])
	}
	if data["command"] != string(turn.CommandModelRequestStarted) {
		t.Fatalf("event command = %#v", data["command"])
	}
}

func TestTurnStateProjectsDurableRetryAndSafeTerminalError(t *testing.T) {
	retrying := turn.CoordinatorSnapshot{
		TurnID: "turn-503", StepID: "step-503", Generation: 1,
		TurnStatus: turn.TurnStatusRunning, StepStatus: turn.StepStatusRequesting,
		HasActiveTurn: true, ModelAttempt: 2, ModelAttemptStatus: turn.ModelAttemptStatusRetrying,
		ModelMaxAttempts: 3, ModelRetryAfterMS: 4200, ModelErrorKind: "provider_unavailable", ModelRetryable: true,
	}
	retryView := buildTurnStateView(retrying, 7)
	if retryView.ModelAttempt == nil || retryView.ModelAttempt.Status != "retrying" || retryView.ModelAttempt.Attempt != 2 || retryView.ModelAttempt.MaxAttempts != 3 || retryView.ModelAttempt.RetryAfterMS != 4200 {
		t.Fatalf("retry projection = %#v", retryView.ModelAttempt)
	}

	failed := retrying
	failed.TurnStatus = turn.TurnStatusFailed
	failed.StepStatus = turn.StepStatusFailed
	failed.TurnEndReason = "模型服务暂时不可用，请稍后重试。"
	failed.ModelAttemptStatus = turn.ModelAttemptStatusFailed
	failed.HasActiveTurn = false
	view := buildTurnStateView(failed, 8)
	if view.Error == nil || view.Error.Code != "provider_unavailable" || view.Error.Message != failed.TurnEndReason || !view.Error.Retryable {
		t.Fatalf("terminal error projection = %#v", view.Error)
	}
	event := turnStateEventData(failed, 8, 0, turn.CommandFailTurn)
	if !reflect.DeepEqual(event["error"], view.Error) || !reflect.DeepEqual(event["model_attempt"], view.ModelAttempt) {
		t.Fatalf("SSE contract differs from hydrate: event=%#v view=%#v", event, view)
	}
}
