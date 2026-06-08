package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	return NewManager("agent-1", stream.NewHub(32, logx.Discard()), &llm.MockClient{}, reg, pol, nil, TurnOptions{SkillsEnabled: false, CompressionBlocking: 0}, logx.Discard())
}

func TestCreateAndList(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	s1, created, err := mgr.Create("")
	if err != nil || !created {
		t.Fatalf("Create: created=%v err=%v", created, err)
	}
	s2, created2, err := mgr.Create(s1.ID)
	if err != nil || created2 {
		t.Fatalf("reuse: created=%v err=%v", created2, err)
	}
	if s2.ID != s1.ID {
		t.Fatal("id mismatch")
	}
	if len(mgr.ListActive()) != 1 {
		t.Fatalf("list len = %d", len(mgr.ListActive()))
	}
}

func TestEnqueueMessageTurn(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg, _ := tools.NewRegistry(t.TempDir(), 30)
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", hub, &llm.MockClient{}, reg, pol, nil, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}

	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "hello", nil); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	var assistantText string
	gotDone := false
	for !gotDone {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "assistant":
				if c, ok := ev.Data["content"].(string); ok {
					assistantText += c
				}
			case "done":
				gotDone = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for done")
		}
	}
	if assistantText != "hello" {
		t.Fatalf("assistant = %q", assistantText)
	}
}

func TestEnqueueMessageSessionNotFound(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()
	if _, err := mgr.EnqueueMessage(context.Background(), "missing", "message", "x", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestCancelTurn(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg, _ := tools.NewRegistry(t.TempDir(), 30)
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", hub, &slowMockLLM{delay: 500 * time.Millisecond}, reg, pol, nil, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	s, _, _ := mgr.Create("")
	_, _ = mgr.EnqueueMessage(context.Background(), s.ID, "message", "long", nil)

	time.Sleep(30 * time.Millisecond)
	if !mgr.CancelTurn(s.ID) {
		t.Fatal("expected cancel true")
	}
}

type slowMockLLM struct {
	delay time.Duration
}

func (s *slowMockLLM) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	select {
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	case <-time.After(s.delay):
	}
	if handler.OnDelta != nil {
		handler.OnDelta("x")
	}
	return llm.ChatResult{Content: "x", FinishReason: "stop"}, nil
}

func (s *slowMockLLM) CompleteText(_ context.Context, _ llm.CompleteRequest) (string, error) {
	return "mock summary", nil
}

func (s *slowMockLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestPersistAfterTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	hub := stream.NewHub(32, logx.Discard())
	reg, _ := tools.NewRegistry(t.TempDir(), 30)
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", hub, &llm.MockClient{}, reg, pol, st, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "persist-me", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		count, _, _ := mgr.ContextSummary(s.ID)
		if count >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for persist")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	mgr.Stop()
	time.Sleep(50 * time.Millisecond)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var st2 *store.SQLiteStore
	for attempt := 0; attempt < 5; attempt++ {
		st2, err = store.Open(path)
		if err == nil {
			break
		}
		if attempt == 4 {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer st2.Close()
	rec, err := st2.Load(context.Background(), s.ID)
	if err != nil || rec == nil || len(rec.Messages) < 2 {
		t.Fatalf("rec=%+v err=%v", rec, err)
	}
}

func TestRestoreSessionFromStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	msgs := []llm.Message{{Role: "user", Content: "old"}, {Role: "assistant", Content: "ok"}}
	if err := st.Save(ctx, store.Record{SessionID: "sess-restore", AgentID: "agent-1", Messages: msgs}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st2, _ := store.Open(path)
	defer st2.Close()
	reg, _ := tools.NewRegistry(t.TempDir(), 30)
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), &llm.MockClient{}, reg, pol, st2, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	s, created, err := mgr.Create("sess-restore")
	if err != nil || created {
		t.Fatalf("restore: created=%v err=%v", created, err)
	}
	count, messages, err := mgr.ContextSummary("sess-restore")
	if err != nil || count != 2 || len(messages) != 2 {
		t.Fatalf("count=%d msgs=%d err=%v", count, len(messages), err)
	}
	_ = s
}
