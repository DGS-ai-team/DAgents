package session

import (
	"fmt"
	"strings"
	"testing"
)

// P0 target ownership tests intentionally use a distinct file so the API and
// active-turn suites can evolve independently.
func TestP0TriggerTargetAgentNeverFallsBackToLocalRuntime(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()
	s := &TriggerSubmitter{Mgr: mgr, EnsureAgentRuntime: func(id string) error {
		if id == "agent-b" {
			return nil
		}
		return nil
	}}
	if _, err := s.EnsureSessionForAgent("agent-b", ""); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("missing target runtime error = %v", err)
	}
	if mgr.Get("agent-b") != nil {
		t.Fatal("target B unexpectedly created a local fallback session")
	}
}

func TestP0TriggerTargetRejectsUnknownOrConflictingSession(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()
	unknown := &TriggerSubmitter{Mgr: mgr, EnsureAgentRuntime: func(id string) error { return fmt.Errorf("unknown target: %s", id) }}
	if _, err := unknown.EnsureSessionForAgent("agent-missing", ""); err == nil {
		t.Fatal("unknown target unexpectedly succeeded")
	}
	s := &TriggerSubmitter{Mgr: mgr, EnsureAgentRuntime: func(id string) error { return nil }}
	local, _, err := mgr.Create("local-session")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureSessionForAgent("agent-b", local.ID); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("conflicting session error = %v", err)
	}
}
