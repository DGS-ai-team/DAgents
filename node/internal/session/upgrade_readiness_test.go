package session

import (
	"testing"
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
