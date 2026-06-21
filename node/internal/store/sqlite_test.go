package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	msgs := []llm.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	if err := s.Save(ctx, Record{SessionID: "sess-1", AgentID: "a1", Messages: msgs}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Load(ctx, "sess-1")
	if err != nil || rec == nil || len(rec.Messages) != 2 {
		t.Fatalf("load = %+v err=%v", rec, err)
	}
	if rec.FirstUserMessage != "hello" {
		t.Fatalf("first = %q", rec.FirstUserMessage)
	}
}

func TestClearAndDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	_ = s.Save(ctx, Record{SessionID: "s1", AgentID: "a", Messages: []llm.Message{{Role: "user", Content: "x"}}})
	if err := s.ClearMessages(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	rec, _ := s.Load(ctx, "s1")
	if len(rec.Messages) != 0 {
		t.Fatal("expected empty messages")
	}
	ok, err := s.Delete(ctx, "s1")
	if err != nil || !ok {
		t.Fatalf("delete ok=%v err=%v", ok, err)
	}
}

func TestSaveLoadRuntimeState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	pending := &turn.PendingHITL{
		Items: []turn.PendingHITLItem{{
			ToolCall: llm.ToolCall{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}},
		}},
	}
	if err := s.Save(ctx, Record{
		SessionID:    "sess-pending",
		AgentID:      "a1",
		Messages:     []llm.Message{{Role: "user", Content: "hi"}},
		RuntimeState: RuntimeState{Pending: pending, ToolLoopCount: 2},
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Load(ctx, "sess-pending")
	if err != nil || rec == nil || rec.RuntimeState.Pending == nil {
		t.Fatalf("load pending = %+v err=%v", rec, err)
	}
	if rec.RuntimeState.ToolLoopCount != 2 {
		t.Fatalf("loop count = %d", rec.RuntimeState.ToolLoopCount)
	}
}

func TestList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	s, _ := Open(path)
	defer s.Close()
	ctx := context.Background()
	_ = s.Save(ctx, Record{SessionID: "s1", AgentID: "a", Messages: []llm.Message{{Role: "user", Content: "a"}}, UpdatedAt: time.Now()})
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}
