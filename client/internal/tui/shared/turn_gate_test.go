package shared

import (
	"context"
	"testing"
	"time"
)

func TestTurnGateIgnoresStaleDoneBeforeContent(t *testing.T) {
	g := NewTurnGate()
	g.NoteSeq(10)
	g.BeginSubmit()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := g.Wait(context.Background()); err != nil {
			t.Errorf("wait: %v", err)
		}
	}()

	// 在途 trigger 的陈旧 done（seq<=fence）不应结束等待。
	if g.ShouldAcceptDone(10) {
		t.Fatal("stale done should not be accepted")
	}
	if !g.Awaiting() {
		t.Fatal("still awaiting after stale done")
	}
	g.NoteSeq(11)
	g.MarkTurnContent()
	g.FinishTurn()
	<-done
}

func TestTurnGateDoneAfterContent(t *testing.T) {
	g := NewTurnGate()
	g.NoteSeq(5)
	g.BeginSubmit()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = g.Wait(context.Background())
	}()

	g.NoteSeq(6)
	g.MarkTurnContent()
	if !g.ShouldAcceptDone(7) {
		t.Fatal("done after content should be accepted")
	}
	g.FinishTurn()

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("wait timeout")
	}
}

func TestTurnGateHITLPauseDoneWithoutAssistant(t *testing.T) {
	g := NewTurnGate()
	g.NoteSeq(20)
	g.BeginSubmit()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = g.Wait(context.Background())
	}()

	// tool_call 标记内容后 HITL done；或 seq>fence 单独即可。
	g.NoteSeq(21)
	g.MarkTurnContent()
	if !g.ShouldAcceptDone(21) {
		t.Fatal("HITL pause done should be accepted")
	}
	g.FinishTurn()

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("wait timeout")
	}
}

func TestTurnGateDoneBySeqAboveFence(t *testing.T) {
	g := NewTurnGate()
	g.NoteSeq(30)
	g.BeginSubmit()

	if g.ShouldAcceptDone(30) {
		t.Fatal("seq==fence without content should not accept")
	}
	if !g.ShouldAcceptDone(31) {
		t.Fatal("seq>fence should accept done even without content marker")
	}
}

func TestTurnGateIsStale(t *testing.T) {
	g := NewTurnGate()
	g.NoteSeq(42)
	g.BeginSubmit()
	if !g.IsStale(42) {
		t.Fatal("seq at fence should be stale")
	}
	if g.IsStale(43) {
		t.Fatal("seq above fence should not be stale")
	}
}
