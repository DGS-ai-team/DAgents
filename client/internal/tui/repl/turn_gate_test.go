package repl

import (
	"context"
	"strings"
	"sync"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func TestTurnGateWaitFinish(t *testing.T) {
	g := tuishared.NewTurnGate()
	g.BeginSubmit()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := g.Wait(context.Background()); err != nil {
			t.Errorf("wait: %v", err)
		}
	}()
	g.FinishTurn()
	<-done
}

func TestHandleEventDoneFinishesAssistantAndSignalsTurn(t *testing.T) {
	show := false
	turn := tuishared.NewTurnGate()
	turn.BeginSubmit()
	r := newStreamRunner(
		nil,
		"sess-1",
		tuishared.NewTranscript(0),
		&tuishared.ToolFold{},
		&sync.Mutex{},
		&show,
		turn,
	)
	ctx := context.Background()

	_, err := r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "assistant",
		Seq:  1,
		Data: map[string]any{"content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		_ = turn.Wait(context.Background())
	}()
	_, err = r.handleEvent(ctx, nodeapi.StreamEvent{Type: "done", Seq: 2, Data: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	<-waitDone
	lines := r.transcript.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], "[assistant] hello") {
		t.Fatalf("assistant line = %v", lines)
	}
}

func TestHandleEventStaleDoneDoesNotSignalTurn(t *testing.T) {
	show := false
	turn := tuishared.NewTurnGate()
	turn.NoteSeq(10)
	turn.BeginSubmit()
	r := newStreamRunner(
		nil,
		"sess-1",
		tuishared.NewTranscript(0),
		&tuishared.ToolFold{},
		&sync.Mutex{},
		&show,
		turn,
	)
	ctx := context.Background()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		_ = turn.Wait(ctx)
	}()

	_, err := r.handleEvent(ctx, nodeapi.StreamEvent{Type: "done", Seq: 10, Data: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !turn.Awaiting() {
		t.Fatal("stale done should not finish turn")
	}

	_, err = r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "assistant",
		Seq:  11,
		Data: map[string]any{"content": "ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.handleEvent(ctx, nodeapi.StreamEvent{Type: "done", Seq: 12, Data: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	<-waitDone
}

func TestHandleEventChildDoneDoesNotSignalTurn(t *testing.T) {
	show := false
	turn := tuishared.NewTurnGate()
	turn.BeginSubmit()
	r := newStreamRunner(
		nil,
		"sess-1",
		tuishared.NewTranscript(0),
		&tuishared.ToolFold{},
		&sync.Mutex{},
		&show,
		turn,
	)
	ctx := context.Background()

	_, err := r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "done",
		Seq:  5,
		Data: map[string]any{"child_session_id": "child-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !turn.Awaiting() {
		t.Fatal("child done should not signal parent turn gate")
	}
}
