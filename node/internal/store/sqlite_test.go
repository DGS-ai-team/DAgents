package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
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
	if err := s.Save(ctx, Record{AgentID: "sess-1", NodeID: "a1", Messages: msgs}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Load(ctx, "sess-1")
	if err != nil || rec == nil || len(rec.Messages) != 2 {
		t.Fatalf("load = %+v err=%v", rec, err)
	}
	if rec.FirstUserMessage != "hello" {
		t.Fatalf("first = %q", rec.FirstUserMessage)
	}
	if rec.NodeID != "a1" {
		t.Fatalf("node_id = %q", rec.NodeID)
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
	_ = s.Save(ctx, Record{AgentID: "s1", NodeID: "a", Messages: []llm.Message{{Role: "user", Content: "x"}}})
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
	if err := s.Save(ctx, Record{
		AgentID:  "sess-pending",
		NodeID:   "a1",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
		RuntimeState: RuntimeState{
			InputBoxState: json.RawMessage(`{"seq":2,"items":[]}`),
		},
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Load(ctx, "sess-pending")
	if err != nil || rec == nil {
		t.Fatalf("load runtime state = %+v err=%v", rec, err)
	}
	if string(rec.RuntimeState.InputBoxState) != `{"seq":2,"items":[]}` {
		t.Fatalf("input box state = %s", rec.RuntimeState.InputBoxState)
	}
}

func TestList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	s, _ := Open(path)
	defer s.Close()
	ctx := context.Background()
	_ = s.Save(ctx, Record{AgentID: "s1", NodeID: "a", Messages: []llm.Message{{Role: "user", Content: "a"}}, UpdatedAt: time.Now()})
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if list[0].AgentID != "s1" || list[0].NodeID != "a" {
		t.Fatalf("summary = %+v", list[0])
	}
}

func TestExecutionEventsPersistAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	code := 0
	ctx := context.Background()
	if err := s.AppendExecutionEvent(ctx, ExecutionEventRecord{
		AgentID:        "agent-1",
		SessionID:      "session-1",
		ProcessID:      "local-process-1",
		ProcessSeq:     1,
		EventType:      "process_started",
		ToolCallID:     "call-1",
		TargetKind:     "local",
		PolicyDecision: "auto",
		CommandDigest:  "digest-1",
		OutputBytes:    7,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendExecutionEvent(ctx, ExecutionEventRecord{
		AgentID:    "agent-1",
		SessionID:  "session-1",
		ProcessID:  "local-process-1",
		ProcessSeq: 2,
		EventType:  "process_exited",
		ToolCallID: "call-1",
		ExitCode:   &code,
	}); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListExecutionEvents(ctx, "session-1", 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if events[0].ProcessSeq != 1 || events[1].ProcessSeq != 2 {
		t.Fatalf("events not ordered: %+v", events)
	}
	if events[1].ExitCode == nil || *events[1].ExitCode != 0 {
		t.Fatalf("exit code=%v", events[1].ExitCode)
	}
	if events[0].CommandDigest != "digest-1" || events[0].OutputBytes != 7 {
		t.Fatalf("audit fields=%+v", events[0])
	}
}
