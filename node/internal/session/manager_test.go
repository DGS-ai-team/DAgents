package session

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
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
	pol := policy.NewDefaultEngine()
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
			case "turn_finished":
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
	pol := policy.NewDefaultEngine()
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
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", hub, &slowMockLLM{delay: 500 * time.Millisecond}, reg, pol, nil, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	s, _, _ := mgr.Create("")
	_, _ = mgr.EnqueueMessage(context.Background(), s.ID, "message", "long", nil, nil, "")

	time.Sleep(30 * time.Millisecond)
	if !mgr.CancelTurn(s.ID) {
		t.Fatal("expected cancel true")
	}
}

func TestClearContextDoesNotRestoreStaleTurnBeforeFirstNewHumanMessage(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg, _ := tools.NewRegistry(t.TempDir(), 30)
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", hub, &slowMockLLM{delay: 2 * time.Second}, reg, pol, nil, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, queue.RequestTypeMessage, "stale-before-clear", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	deadline := time.Now().Add(2 * time.Second)
	for !rt.turnCoordinator.Snapshot().HasActiveTurn {
		if time.Now().After(deadline) {
			t.Fatal("turn did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := mgr.ClearContext(sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, queue.RequestTypeMessage, "first-after-clear", nil, nil, ""); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(3 * time.Second)
	for {
		messages := rt.messagesSnapshot()
		if len(messages) >= 2 {
			if messages[0].Role != "user" || messages[0].Content != "first-after-clear" {
				t.Fatalf("stale history restored before first post-clear message: %+v", messages)
			}
			if len(messages) != 2 || messages[1].Role != "assistant" || messages[1].Content != "x" {
				t.Fatalf("unexpected post-clear history: %+v", messages)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("post-clear message was not processed: %+v", messages)
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
	pol := policy.NewDefaultEngine()
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

func TestPersistTurnLifecycleBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lifecycle.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", stream.NewHub(32, logx.Discard()), &llm.MockClient{}, reg, pol, st, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "lifecycle", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var events []turn.TurnEventEnvelope
	for time.Now().Before(deadline) {
		events, err = st.ListTurnEvents(context.Background(), s.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.EventType == turn.EventTurnCompleted {
				break
			}
		}
		if len(events) >= 8 && events[len(events)-1].EventType == turn.EventTurnCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	seen := make(map[turn.EventType]bool)
	for _, event := range events {
		seen[event.EventType] = true
	}
	for _, eventType := range []turn.EventType{
		turn.EventTurnStarted, turn.EventStepStarted, turn.EventTurnSnapshotCreated,
		turn.EventModelRequestStarted, turn.EventModelRequestCompleted,
		turn.EventAssistantMessageRecorded, turn.EventStepCompleted, turn.EventTurnCompleted,
	} {
		if !seen[eventType] {
			t.Fatalf("missing lifecycle event %s in %#v", eventType, events)
		}
	}
	lastSequence := events[len(events)-1].SessionSeq
	mgr.Stop()
	reloaded := NewManager("agent-1", stream.NewHub(32, logx.Discard()), &llm.MockClient{}, reg, pol, st, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer reloaded.Stop()
	if _, created, err := reloaded.Create(s.ID); err != nil || created {
		t.Fatalf("restore lifecycle runtime: created=%v err=%v", created, err)
	}
	rt := reloaded.getRuntime(s.ID)
	if rt == nil {
		t.Fatal("restored lifecycle runtime is nil")
	}
	if rt.lifecycleEventSequence() != lastSequence {
		t.Fatalf("restored lifecycle sequence=%v want=%v", rt.lifecycleEventSequence(), lastSequence)
	}
	if snapshot := rt.turnCoordinator.Snapshot(); snapshot.TurnStatus != turn.TurnStatusCompleted || snapshot.StepStatus != turn.StepStatusCompleted {
		t.Fatalf("restored terminal lifecycle projection=%+v", snapshot)
	}
}

func TestPersistTurnLifecycleToolBatchBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-lifecycle.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", stream.NewHub(32, logx.Discard()), &llm.MockClient{EnableTools: true}, reg, pol, st, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "read", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var events []turn.TurnEventEnvelope
	for time.Now().Before(deadline) {
		events, err = st.ListTurnEvents(context.Background(), s.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) > 0 && events[len(events)-1].EventType == turn.EventTurnCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	seen := make(map[turn.EventType]bool)
	for _, event := range events {
		seen[event.EventType] = true
	}
	for _, eventType := range []turn.EventType{
		turn.EventToolCallRecorded, turn.EventToolBatchCreated,
		turn.EventToolExecutionStarted, turn.EventToolExecutionFailed,
		turn.EventToolResultRecorded, turn.EventToolBatchSettled,
	} {
		if !seen[eventType] {
			t.Fatalf("missing tool lifecycle event %s in %#v", eventType, events)
		}
	}
	recovered := turn.NewTurnCoordinator(s.ID, "agent-1")
	if err := recovered.Restore(events); err != nil {
		t.Fatalf("restore tool lifecycle events: %v", err)
	}
	if snapshot := recovered.Snapshot(); snapshot.TurnStatus != turn.TurnStatusCompleted || snapshot.StepIndex != 2 {
		t.Fatalf("restored tool lifecycle projection = %#v", snapshot)
	} else if snapshot.ContextSnapshot == nil || snapshot.ContextSnapshot.SystemPrompt == "" {
		t.Fatalf("restored model context snapshot = %#v", snapshot.ContextSnapshot)
	} else if snapshot.ContextSnapshot.ContextInjectionDigest == "" || len(snapshot.ContextSnapshot.ContextInjections) == 0 {
		t.Fatalf("restored context injection snapshot = %#v", snapshot.ContextSnapshot)
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
	pol := policy.NewDefaultEngine()
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

func TestLoadSessionDataReportsEmptyRecordWithoutSecondLookup(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "empty-session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Save(context.Background(), store.Record{AgentID: "sess-empty", NodeID: "agent-1"}); err != nil {
		t.Fatal(err)
	}

	m := &Manager{store: st, logger: logx.Discard()}
	existing, err := m.loadSessionData("sess-empty")
	if err != nil {
		t.Fatal(err)
	}
	if !existing.Found {
		t.Fatal("empty persisted session was reported as missing")
	}

	missing, err := m.loadSessionData("sess-missing")
	if err != nil {
		t.Fatal(err)
	}
	if missing.Found {
		t.Fatal("missing session was reported as found")
	}
}

type captureMultimodalLLM struct {
	mu           sync.RWMutex
	lastMessages []llm.Message
	ready        chan struct{}
	readyOnce    sync.Once
}

func (c *captureMultimodalLLM) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.lastMessages = append([]llm.Message(nil), req.Messages...)
	if c.ready != nil {
		c.readyOnce.Do(func() { close(c.ready) })
	}
	c.mu.Unlock()
	if handler.OnDelta != nil {
		handler.OnDelta("ok")
	}
	return llm.ChatResult{Content: "ok", FinishReason: "stop"}, nil
}

func (c *captureMultimodalLLM) messages() []llm.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]llm.Message(nil), c.lastMessages...)
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
	pol := policy.NewDefaultEngine()
	capture := &captureMultimodalLLM{ready: make(chan struct{})}
	mgr := NewManager("agent-1", hub, capture, reg, pol, nil, TurnOptions{
		WorkspaceRoot:     fsRoot,
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

	select {
	case <-capture.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for llm call")
	}

	var userMsg *llm.Message
	messages := capture.messages()
	for i := range messages {
		m := &messages[i]
		if m.Role == "user" && llm.MessageHasImages(*m) {
			userMsg = m
			break
		}
	}
	if userMsg == nil {
		t.Fatalf("no multimodal user message in llm request: %+v", messages)
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
