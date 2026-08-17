package store

import (
	"context"
	"database/sql"
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
	pending := &turn.PendingHITL{
		Items: []turn.PendingHITLItem{{
			ToolCall: llm.ToolCall{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}},
		}},
	}
	if err := s.Save(ctx, Record{
		AgentID:      "sess-pending",
		NodeID:       "a1",
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
}

func TestMigrateLegacySessionsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE sessions (
  session_id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  messages_json TEXT NOT NULL DEFAULT '[]',
  first_user_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  loaded_skills_json TEXT NOT NULL DEFAULT '[]',
  runtime_state_json TEXT NOT NULL DEFAULT '{}'
);
INSERT INTO sessions(session_id, agent_id, messages_json, first_user_message, created_at, updated_at)
VALUES ('agt-legacy', 'node-1', '[{"role":"user","content":"hi"}]', 'hi', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z');
`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rec, err := s.Load(context.Background(), "agt-legacy")
	if err != nil || rec == nil {
		t.Fatalf("load migrated = %+v err=%v", rec, err)
	}
	if rec.AgentID != "agt-legacy" || rec.NodeID != "node-1" {
		t.Fatalf("ids = agent=%q node=%q", rec.AgentID, rec.NodeID)
	}
	if len(rec.Messages) != 1 || rec.FirstUserMessage != "hi" {
		t.Fatalf("payload = %+v", rec)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("legacy sessions table should be dropped")
	}
}
