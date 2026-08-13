package session

import (
	"context"
	"path/filepath"
	"strings"
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
	return NewManager("agent-1", stream.NewHub(32, logx.Discard()), &llm.MockClient{}, reg, pol, nil, TurnOptions{SkillsEnabled: false, CompressionBlocking: 0, MultimodalEnabled: true}, logx.Discard())
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

	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "hello", nil, nil, ""); err != nil {
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
	if _, err := mgr.EnqueueMessage(context.Background(), "missing", "message", "x", nil, nil, ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnqueueMessageMultimodal(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()
	s, _, _ := mgr.Create("")
	parts := []llm.ContentPart{{
		Type:     "image_url",
		ImageURL: &llm.ImageURLPart{URL: "https://example.com/a.png"},
	}}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "describe this", parts, nil, ""); err != nil {
		t.Fatalf("enqueue multimodal: %v", err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "", nil, nil, ""); err == nil {
		t.Fatal("expected invalid empty message")
	}
}

func TestEnqueueMessageMultimodalDisabled(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", stream.NewHub(32, logx.Discard()), &llm.MockClient{}, reg, pol, nil, TurnOptions{MultimodalEnabled: false}, logx.Discard())
	defer mgr.Stop()
	s, _, _ := mgr.Create("")
	parts := []llm.ContentPart{{
		Type:     "image_url",
		ImageURL: &llm.ImageURLPart{URL: "https://example.com/a.png"},
	}}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "describe this", parts, nil, ""); err == nil {
		t.Fatal("expected multimodal_disabled")
	} else if err.Error() != "multimodal_disabled" {
		t.Fatalf("err = %q, want multimodal_disabled", err)
	}
}

func TestCancelTurn(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg, _ := tools.NewRegistry(t.TempDir(), 30)
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", hub, &slowMockLLM{delay: 500 * time.Millisecond}, reg, pol, nil, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	s, _, _ := mgr.Create("")
	_, _ = mgr.EnqueueMessage(context.Background(), s.ID, "message", "long", nil, nil, "")

	time.Sleep(30 * time.Millisecond)
	if !mgr.CancelTurn(s.ID) {
		t.Fatal("expected cancel true")
	}
}

func TestCancelTurnCancelsDetachedBashJobs(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	reg := mgr.SessionTools(s.ID)
	if reg == nil {
		t.Fatal("session tool registry is nil")
	}
	if _, err := reg.StartBackground(tools.WithSession(context.Background(), s.ID), s.ID, "bash_run", "call-detached", `{"command":"sleep 30"}`); err != nil {
		t.Fatalf("start background bash: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		counts := reg.SessionToolJobCounts(s.ID)
		if counts.Background > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background job not registered: %+v", counts)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !mgr.CancelTurn(s.ID) {
		t.Fatal("expected cancel true for detached background job")
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		counts := reg.SessionToolJobCounts(s.ID)
		if counts.Running == 0 && counts.Background == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("background job still active after turn cancel: %+v", counts)
		}
		time.Sleep(20 * time.Millisecond)
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
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "persist-me", nil, nil, ""); err != nil {
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
	if err := st.Save(ctx, store.Record{AgentID: "sess-restore", NodeID: "agent-1", Messages: msgs}); err != nil {
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

type captureMultimodalLLM struct {
	lastMessages []llm.Message
}

func (c *captureMultimodalLLM) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.lastMessages = append([]llm.Message(nil), req.Messages...)
	if handler.OnDelta != nil {
		handler.OnDelta("ok")
	}
	return llm.ChatResult{Content: "ok", FinishReason: "stop"}, nil
}

func (c *captureMultimodalLLM) CompleteText(_ context.Context, _ llm.CompleteRequest) (string, error) {
	return "mock summary", nil
}

func (c *captureMultimodalLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestHumanMessageImageExpandedForLLM(t *testing.T) {
	fsRoot := t.TempDir()
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(fsRoot, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	capture := &captureMultimodalLLM{}
	mgr := NewManager("agent-1", hub, capture, reg, pol, nil, TurnOptions{
		FSRoot:            fsRoot,
		SkillsEnabled:     false,
		MultimodalEnabled: true,
	}, logx.Discard())
	defer mgr.Stop()

	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}

	dataURL := "data:image/png;base64,iVBORw0KGgo="
	parts := []llm.ContentPart{{
		Type:     "image_url",
		ImageURL: &llm.ImageURLPart{URL: dataURL},
	}}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "这个是什么图片", parts, nil, ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for capture.lastMessages == nil {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for llm call")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	var userMsg *llm.Message
	for i := range capture.lastMessages {
		m := &capture.lastMessages[i]
		if m.Role == "user" && llm.MessageHasImages(*m) {
			userMsg = m
			break
		}
	}
	if userMsg == nil {
		t.Fatalf("no multimodal user message in llm request: %+v", capture.lastMessages)
	}
	gotURL := ""
	for _, part := range userMsg.ContentParts {
		if part.Type == "image_url" && part.ImageURL != nil {
			gotURL = part.ImageURL.URL
			break
		}
	}
	if !strings.HasPrefix(gotURL, "data:image/png;base64,") {
		t.Fatalf("expected expanded data url, got %q parts=%+v", gotURL, userMsg.ContentParts)
	}
}
