package queue

import (
	"context"
	"testing"
	"time"
)

func TestPriorityOrder(t *testing.T) {
	q := NewMessageQueue()
	_ = q.Enqueue(Envelope{RequestType: "message", Content: "other"}, PriorityOther)
	_ = q.Enqueue(Envelope{RequestType: "message", Content: "human"}, PriorityHuman)
	_ = q.Enqueue(Envelope{RequestType: "resume"}, PriorityResume)

	ctx := context.Background()
	first, err := q.Dequeue(ctx)
	if err != nil || first.Content != "human" {
		t.Fatalf("first = %+v err=%v", first, err)
	}
	second, err := q.Dequeue(ctx)
	if err != nil || second.RequestType != "resume" {
		t.Fatalf("second = %+v err=%v", second, err)
	}
	third, err := q.Dequeue(ctx)
	if err != nil || third.Content != "other" {
		t.Fatalf("third = %+v err=%v", third, err)
	}
}

func TestDequeueContextCancel(t *testing.T) {
	q := NewMessageQueue()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := q.Dequeue(ctx)
	if err == nil {
		t.Fatal("expected cancel")
	}
}

func TestParsePriority(t *testing.T) {
	if p, ok := ParsePriority("human"); !ok || p != PriorityHuman {
		t.Fatalf("human = %q ok=%v", p, ok)
	}
	if _, ok := ParsePriority("unknown"); ok {
		t.Fatal("unknown priority should fail")
	}
}
