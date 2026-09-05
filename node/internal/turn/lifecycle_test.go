package turn

import (
	"strings"
	"testing"
	"time"
)

func TestTurnLifecycleSupportsSuspendResumeAndCompletion(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	tn, err := NewTurn("turn-1", "session-1", "agent-1", TurnSourceHuman)
	if err != nil {
		t.Fatal(err)
	}
	if err := tn.Advance(EventTurnStarted, now, ""); err != nil {
		t.Fatal(err)
	}
	step, err := tn.StartStep("step-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := step.Advance(EventStepStarted, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := step.Advance(EventAssistantMessageRecorded, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := step.Advance(EventToolBatchCreated, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := tn.Advance(EventInteractionRequested, now, "approval required"); err != nil {
		t.Fatal(err)
	}
	if err := step.Advance(EventInteractionRequested, now, "approval required"); err != nil {
		t.Fatal(err)
	}
	if tn.Status != TurnStatusWaiting || step.Status != StepStatusWaitingInteraction {
		t.Fatalf("waiting state = turn:%s step:%s", tn.Status, step.Status)
	}
	if err := tn.Advance(EventInteractionResolved, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := step.Advance(EventInteractionResolved, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := step.Advance(EventToolBatchSettled, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := step.Advance(EventStepCompleted, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := tn.Advance(EventTurnCompleted, now.Add(time.Second), "completed"); err != nil {
		t.Fatal(err)
	}
	if tn.Status != TurnStatusCompleted || tn.FinishedAt == nil {
		t.Fatalf("turn did not complete: %#v", tn)
	}
}

func TestTurnLifecycleRejectsTerminalTransitions(t *testing.T) {
	tn, err := NewTurn("turn-1", "session-1", "agent-1", TurnSourceHuman)
	if err != nil {
		t.Fatal(err)
	}
	if err := tn.Advance(EventTurnStarted, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	if err := tn.Advance(EventTurnCompleted, time.Now(), "done"); err != nil {
		t.Fatal(err)
	}
	if err := tn.Advance(EventTurnStarted, time.Now(), ""); err == nil || !strings.Contains(err.Error(), "invalid turn transition") {
		t.Fatalf("expected terminal transition error, got %v", err)
	}
}

func TestTurnStartStepRequiresRunningTurn(t *testing.T) {
	tn, err := NewTurn("turn-1", "session-1", "agent-1", TurnSourceHuman)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.StartStep("step-1", time.Now()); err == nil {
		t.Fatal("expected StartStep to reject a non-running turn")
	}
}

func TestLifecycleConstructorsValidateIdentity(t *testing.T) {
	if _, err := NewTurn("", "session-1", "agent-1", TurnSourceHuman); err == nil {
		t.Fatal("expected missing turn id error")
	}
	if _, err := NewStep("step-1", "turn-1", 0, 0); err == nil {
		t.Fatal("expected invalid step index error")
	}
	if _, err := NewStep("step-1", "turn-1", 1, -1); err == nil {
		t.Fatal("expected invalid context epoch error")
	}
}

func TestNextStepStatusAllowsTurnCancellationFromEveryNonTerminalPhase(t *testing.T) {
	for _, status := range []StepStatus{
		StepStatusCreated,
		StepStatusRequesting,
		StepStatusAssistantReceived,
		StepStatusExecutingTools,
		StepStatusWaitingInteraction,
		StepStatusReadyForNext,
	} {
		next, ok := NextStepStatus(status, EventStepCancelled)
		if !ok || next != StepStatusCancelled {
			t.Fatalf("cancel transition from %q = (%q, %v), want (%q, true)", status, next, ok, StepStatusCancelled)
		}
	}
}
