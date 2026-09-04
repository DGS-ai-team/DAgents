package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestInputBoxRejectsInvalidAndOversizedInputs(t *testing.T) {
	box := NewInputBox()
	if _, err := box.Append(InputKind("control"), queue.Envelope{Content: "not external"}); !errors.Is(err, ErrInvalidInputKind) {
		t.Fatalf("invalid kind error = %v", err)
	}
	if _, err := box.Append(InputKindUser, queue.Envelope{Content: strings.Repeat("x", InputBoxMaxRecordBytes)}); !errors.Is(err, ErrInputRecordTooBig) {
		t.Fatalf("oversized input error = %v", err)
	}
	for i := 0; i < InputBoxMaxItems; i++ {
		if _, err := box.Append(InputKindUser, queue.Envelope{Content: "bounded"}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if _, err := box.Append(InputKindUser, queue.Envelope{Content: "overflow"}); !errors.Is(err, ErrInputBoxFull) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestInputBoxRestoreRejectsDuplicateSequences(t *testing.T) {
	box := NewInputBox()
	raw := []byte(`{"seq":1,"items":[{"seq":1,"kind":"user","env":{}},{"seq":1,"kind":"trigger","env":{}}]}`)
	if err := box.Restore(raw); err == nil {
		t.Fatal("expected duplicate sequence restore to fail")
	}
}

func TestInputBoxFIFOSequenceAndRestore(t *testing.T) {
	box := NewInputBox()
	first, err := box.Append(InputKindUser, queue.Envelope{Content: "first", SessionEpoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	second, err := box.Append(InputKindTrigger, queue.Envelope{Content: "second", SessionEpoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("sequences = %d, %d; want 1, 2", first, second)
	}

	raw := box.Snapshot()
	restored := NewInputBox()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	got, ok := restored.Pop()
	if !ok || got.Seq != first || got.Kind != InputKindUser || got.Env.Content != "first" {
		t.Fatalf("first restored input = %+v, ok=%v", got, ok)
	}
	got, ok = restored.Pop()
	if !ok || got.Seq != second || got.Kind != InputKindTrigger || got.Env.Content != "second" {
		t.Fatalf("second restored input = %+v, ok=%v", got, ok)
	}
	third, err := restored.Append(InputKindUser, queue.Envelope{Content: "third", SessionEpoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	if third != 3 {
		t.Fatalf("next sequence = %d, want 3", third)
	}
}

func TestInputBoxDropStalePreservesSequence(t *testing.T) {
	box := NewInputBox()
	_, _ = box.Append(InputKindUser, queue.Envelope{Content: "old", SessionEpoch: 1})
	_, _ = box.Append(InputKindUser, queue.Envelope{Content: "new", SessionEpoch: 2})
	if dropped := box.DropStale(2); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	got, ok := box.Pop()
	if !ok || got.Env.Content != "new" {
		t.Fatalf("remaining input = %+v, ok=%v", got, ok)
	}
	seq, err := box.Append(InputKindTrigger, queue.Envelope{Content: "later", SessionEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if seq != 3 {
		t.Fatalf("sequence after drop = %d, want 3", seq)
	}
}

func TestInputBoxInFlightSurvivesRestartAndAcknowledgement(t *testing.T) {
	box := NewInputBox()
	seq, err := box.Append(InputKindUser, queue.Envelope{Content: "recover me", SessionEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := box.Pop(); !ok || got.Seq != seq {
		t.Fatalf("popped input = %+v, ok=%v", got, ok)
	}

	restored := NewInputBox()
	if err := restored.Restore(box.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if got, ok := restored.InFlight(); !ok || got.Seq != seq || got.Completed {
		t.Fatalf("restored in-flight input = %+v, ok=%v", got, ok)
	}
	if !restored.MarkCompleted(seq) {
		t.Fatal("MarkCompleted returned false")
	}
	completed := NewInputBox()
	if err := completed.Restore(restored.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if got, ok := completed.InFlight(); !ok || !got.Completed {
		t.Fatalf("completed in-flight input = %+v, ok=%v", got, ok)
	}
	if !completed.Ack(seq) || completed.Len() != 0 {
		t.Fatal("ack did not clear in-flight input")
	}
}

func TestInputBoxRequeueInFlightPreservesFIFO(t *testing.T) {
	box := NewInputBox()
	first, _ := box.Append(InputKindUser, queue.Envelope{Content: "first"})
	second, _ := box.Append(InputKindUser, queue.Envelope{Content: "second"})
	got, ok := box.Pop()
	if !ok || got.Seq != first {
		t.Fatalf("popped input = %+v, ok=%v", got, ok)
	}
	if !box.RequeueInFlight() {
		t.Fatal("RequeueInFlight returned false")
	}
	got, ok = box.Pop()
	if !ok || got.Seq != first {
		t.Fatalf("requeued input = %+v, ok=%v", got, ok)
	}
	if !box.Ack(first) {
		t.Fatal("ack requeued input returned false")
	}
	got, ok = box.Pop()
	if !ok || got.Seq != second {
		t.Fatalf("second input = %+v, ok=%v", got, ok)
	}
}

func TestActiveTurnDefersPoppedInputWithoutDroppingIt(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	rt := newRuntimeWithPublisher(
		"session-input-race", "agent-1", hub, hub, &llm.MockClient{}, nil, nil, nil,
		logx.Discard(), nil, nil, nil, false, 0, 0, TurnOptions{}, nil,
	)
	if err := rt.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	seq, err := rt.inputBox.Append(InputKindUser, queue.Envelope{Content: "keep me"})
	if err != nil {
		t.Fatal(err)
	}
	record, ok := rt.inputBox.Pop()
	if !ok || record.Seq != seq {
		t.Fatalf("popped input = %+v, ok=%v", record, ok)
	}

	if consumed := rt.dispatchInput(context.Background(), record); consumed {
		t.Fatal("active Turn consumed an input instead of deferring it")
	}
	if _, inFlight := rt.inputBox.InFlight(); inFlight {
		t.Fatal("deferred input remained in-flight")
	}
	if got, ok := rt.inputBox.Peek(); !ok || got.Seq != seq || got.Env.Content != "keep me" {
		t.Fatalf("deferred input = %+v, ok=%v", got, ok)
	}
}
