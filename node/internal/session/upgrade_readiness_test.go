package session

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestUpgradeReadiness_idle(t *testing.T) {
	m := testManager(t)
	if _, _, err := m.Create(""); err != nil {
		t.Fatal(err)
	}
	got := m.UpgradeReadiness()
	if !got.Ready || got.HasActiveTurn || got.ActiveTurnCount != 0 {
		t.Fatalf("got %#v, want ready idle", got)
	}
}

func TestUpgradeReadiness_pendingHITL(t *testing.T) {
	m := testManager(t)
	sess, _, err := m.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := m.getRuntime(sess.ID)
	if err := rt.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	state := rt.turnCoordinator.Snapshot()
	if _, err := rt.lifecycleDispatchErr(turn.TurnCommand{
		Type: turn.CommandAssistantReceived, SessionID: sess.ID,
		TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation,
		HasTools: true, At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.lifecycleDispatchErr(turn.TurnCommand{
		Type: turn.CommandToolCallRecorded, SessionID: sess.ID,
		TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation,
		ToolCallID: "call-1", ToolName: "approval", Arguments: []byte(`{}`), At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	state = rt.turnCoordinator.Snapshot()
	if _, err := rt.lifecycleDispatchErr(turn.TurnCommand{
		Type: turn.CommandInteractionRequested, SessionID: sess.ID,
		TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation,
		InteractionID: "approval-1", InteractionKind: "approval", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	got := m.UpgradeReadiness()
	if got.Ready || !got.HasActiveTurn || got.ActiveTurnCount != 1 {
		t.Fatalf("got %#v, want pending HITL to remain busy", got)
	}
}

func TestUpgradeReadiness_activeTurn(t *testing.T) {
	m := testManager(t)
	sess, _, err := m.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := m.getRuntime(sess.ID)
	if rt == nil {
		t.Fatal("runtime missing")
	}
	if err := rt.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}

	got := m.UpgradeReadiness()
	if got.Ready || !got.HasActiveTurn || got.ActiveTurnCount != 1 {
		t.Fatalf("got %#v, want busy", got)
	}
	if len(got.ActiveSessionIDs) != 1 || got.ActiveSessionIDs[0] != sess.ID {
		t.Fatalf("session ids = %v", got.ActiveSessionIDs)
	}
}
