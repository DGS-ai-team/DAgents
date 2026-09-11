package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestListTurnEventsForTurnBoundedEnforcesEventsBytesAndOrder(t *testing.T) {
	st, err := Open(t.TempDir() + "/events.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 0; i < 3; i++ {
		e := turn.NewTurnEventEnvelope("session", turn.EventTurnStarted, time.Now().UTC())
		e.AgentID = "agent"
		e.TurnID = "turn"
		e.CommandID = "command-" + string(rune('a'+i))
		e.Payload = []byte(`{"value":"` + strings.Repeat("x", i+1) + `"}`)
		if _, err := st.AppendTurnEvent(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.ListTurnEventsForTurnBounded(context.Background(), "session", "turn", 3, 1000)
	if err != nil || len(got) != 3 || got[0].ID >= got[1].ID {
		t.Fatalf("events=%d err=%v", len(got), err)
	}
	if got, err := st.ListTurnEventsForTurnBounded(context.Background(), "session", "turn", 2, 1000); err == nil || got != nil {
		t.Fatal("expected event limit error")
	}
	if got, err := st.ListTurnEventsForTurnBounded(context.Background(), "session", "turn", 3, 2); err == nil || got != nil {
		t.Fatal("expected payload limit error")
	}
	u := turn.NewTurnEventEnvelope("session", turn.EventTurnStarted, time.Now().UTC())
	u.AgentID = "agent"
	u.TurnID = "unicode"
	u.CommandID = "unicode-command"
	u.Payload = []byte(`{"v":"中"}`)
	if _, err := st.AppendTurnEvent(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ListTurnEventsForTurnBounded(context.Background(), "session", "unicode", 1, 9); err == nil || got != nil {
		t.Fatal("expected UTF-8 byte limit error")
	}
	if got, err := st.ListTurnEventsForTurnBounded(context.Background(), "session", "unicode", 1, 11); err != nil || len(got) != 1 {
		t.Fatalf("unicode boundary got=%d err=%v", len(got), err)
	}
	for i := 0; i < 2; i++ {
		e := turn.NewTurnEventEnvelope("session", turn.EventTurnStarted, time.Now().UTC())
		e.AgentID = "agent"
		e.TurnID = "aggregate"
		e.CommandID = "aggregate-" + string(rune('a'+i))
		e.Payload = []byte(`{"v":"中"}`)
		if _, err := st.AppendTurnEvent(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := st.ListTurnEventsForTurnBounded(context.Background(), "session", "aggregate", 2, 15); err == nil || got != nil {
		t.Fatal("expected aggregate payload limit error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := st.ListTurnEventsForTurnBounded(ctx, "session", "turn", 3, 1000); err == nil || got != nil {
		t.Fatal("expected context cancellation")
	}
	if got, err := st.ListTurnEventsForTurnBounded(context.Background(), "session", "turn", 100001, 1000); err == nil || got != nil {
		t.Fatal("expected event limit cap error")
	}
}
