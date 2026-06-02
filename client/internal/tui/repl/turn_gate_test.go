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
	var g turnGate
	g.begin()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := g.wait(context.Background()); err != nil {
			t.Errorf("wait: %v", err)
		}
	}()
	g.finish()
	<-done
}

func TestHandleEventDoneFinishesAssistantAndSignalsTurn(t *testing.T) {
	show := false
	turnDone := false
	r := newStreamRunner(
		nil,
		"sess-1",
		tuishared.NewTranscript(0),
		&tuishared.ToolFold{},
		&sync.Mutex{},
		&show,
		func() { turnDone = true },
	)
	ctx := context.Background()

	_, err := r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "assistant",
		Data: map[string]any{"content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.handleEvent(ctx, nodeapi.StreamEvent{Type: "done", Data: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !turnDone {
		t.Fatal("onTurnDone not called on done")
	}
	lines := r.transcript.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], "[assistant] hello") {
		t.Fatalf("assistant line = %v", lines)
	}
}

func TestHandleEventChildDoneDoesNotSignalTurn(t *testing.T) {
	show := false
	turnDone := false
	r := newStreamRunner(
		nil,
		"sess-1",
		tuishared.NewTranscript(0),
		&tuishared.ToolFold{},
		&sync.Mutex{},
		&show,
		func() { turnDone = true },
	)
	ctx := context.Background()

	_, err := r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "done",
		Data: map[string]any{"child_session_id": "child-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if turnDone {
		t.Fatal("child done should not signal parent turn gate")
	}
}
