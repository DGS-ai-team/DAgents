package session

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/queue"
)

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
