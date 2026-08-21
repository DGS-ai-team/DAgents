package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestTurnEventStoreSequencesAndIdempotency(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	first := turn.NewTurnEventEnvelope("session-1", turn.EventTurnStarted, now)
	first.AgentID = "agent-1"
	first.TurnID = "turn-1"
	first.CommandID = "cmd-1"
	first.Payload = []byte(`{"status":"running"}`)
	stored, err := st.AppendTurnEvent(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID == 0 || stored.SessionSeq != 1 || stored.TurnSeq != 1 {
		t.Fatalf("first event = %#v", stored)
	}

	second := turn.NewTurnEventEnvelope("session-1", turn.EventStepStarted, now.Add(time.Second))
	second.AgentID = "agent-1"
	second.TurnID = "turn-1"
	second.StepID = "step-1"
	second.CommandID = "cmd-2"
	storedSecond, err := st.AppendTurnEvent(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if storedSecond.SessionSeq != 2 || storedSecond.TurnSeq != 2 {
		t.Fatalf("second event = %#v", storedSecond)
	}

	replayed, err := st.AppendTurnEvent(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != storedSecond.ID || replayed.SessionSeq != storedSecond.SessionSeq {
		t.Fatalf("idempotent replay = %#v want %#v", replayed, storedSecond)
	}
	conflict := second
	conflict.EventType = turn.EventStepCompleted
	if _, err := st.AppendTurnEvent(context.Background(), conflict); err == nil {
		t.Fatal("expected command id conflict to be rejected")
	}

	third := turn.NewTurnEventEnvelope("session-1", turn.EventTurnStarted, now.Add(2*time.Second))
	third.AgentID = "agent-1"
	third.TurnID = "turn-2"
	third.CommandID = "cmd-3"
	storedThird, err := st.AppendTurnEvent(context.Background(), third)
	if err != nil {
		t.Fatal(err)
	}
	if storedThird.SessionSeq != 3 || storedThird.TurnSeq != 1 {
		t.Fatalf("new turn event = %#v", storedThird)
	}

	events, err := st.ListTurnEvents(context.Background(), "session-1", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].CommandID != "cmd-2" || events[1].TurnID != "turn-2" {
		t.Fatalf("listed events = %#v", events)
	}
}

func TestTurnEventStoreRejectsNonContiguousExplicitSequence(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	event := turn.NewTurnEventEnvelope("session-1", turn.EventTurnStarted, time.Now().UTC())
	event.TurnID = "turn-1"
	event.CommandID = "cmd-1"
	event.SessionSeq = 4
	event.TurnSeq = 4
	if _, err := st.AppendTurnEvent(context.Background(), event); err == nil {
		t.Fatal("expected sequence validation error")
	}
}
