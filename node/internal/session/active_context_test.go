package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type activeContextCaptureClient struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
	seen     chan struct{}
}

func (c *activeContextCaptureClient) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	if handler.OnDelta != nil {
		handler.OnDelta("done")
	}
	c.seen <- struct{}{}
	return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
}
func (*activeContextCaptureClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*activeContextCaptureClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestResetActiveContextKeepsHistoryAndIsIdempotentAcrossRestart(t *testing.T) {
	db := t.TempDir() + "/runtime.db"
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", nil, &llm.MockClient{}, nil, nil, st, TurnOptions{}, nil)
	if _, _, err := mgr.Create("session-1"); err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime("session-1")
	rt.mu.Lock()
	rt.messages = []llm.Message{{Role: "user", Content: "old question"}, {Role: "assistant", Content: "old answer"}}
	rt.mu.Unlock()
	if err := rt.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, acquired, err := rt.executionGate.acquireMaintenanceContext(context.Background())
	if err != nil || !acquired {
		t.Fatalf("lease: acquired=%v err=%v", acquired, err)
	}
	boundary, err := mgr.CaptureActiveContextBoundary(leaseCtx, "session-1")
	if err != nil || boundary == "" {
		t.Fatalf("capture boundary: %q err=%v", boundary, err)
	}
	rt.mu.Lock()
	rt.messages = append(rt.messages, llm.Message{Role: "user", Content: "new question"})
	rt.mu.Unlock()
	changed, err := mgr.ResetActiveContext(leaseCtx, "session-1", boundary, "dream-commit-1")
	if err != nil || !changed {
		t.Fatalf("reset: changed=%v err=%v", changed, err)
	}
	rt.mu.Lock()
	if got := rt.activeContextStart; got != 2 {
		t.Fatalf("active start=%d want 2", got)
	}
	rt.mu.Unlock()
	if err := rt.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	retried, err := mgr.ResetActiveContext(leaseCtx, "session-1", boundary, "dream-commit-1")
	if err != nil || retried {
		t.Fatalf("idempotent retry: changed=%v err=%v", retried, err)
	}
	if got := len(rt.activeMessagesSnapshot()); got != 1 || rt.activeMessagesSnapshot()[0].Content != "new question" {
		t.Fatalf("active messages after retry=%#v", rt.activeMessagesSnapshot())
	}
	release()
	mgr.Stop()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := reopened.Load(context.Background(), "session-1")
	if err != nil || restored == nil {
		t.Fatalf("load: record=%v err=%v", restored, err)
	}
	if len(restored.Messages) != 3 || restored.RuntimeState.ActiveContextStart != 2 || restored.RuntimeState.LastContextResetID != "dream-commit-1" {
		t.Fatalf("durable reset state=%+v messages=%d", restored.RuntimeState, len(restored.Messages))
	}
	reopened.Close()
}

func TestResetActiveContextRequiresItsMaintenanceLease(t *testing.T) {
	mgr := NewManager("agent-1", nil, &llm.MockClient{}, nil, nil, nil, TurnOptions{}, nil)
	t.Cleanup(func() { mgr.Stop() })
	if _, _, err := mgr.Create("session-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ResetActiveContext(context.Background(), "session-1", "boundary", "reset-1"); err == nil {
		t.Fatal("reset without maintenance lease succeeded")
	}
}

func TestResetActiveContextNextModelUsesOnlyActiveTail(t *testing.T) {
	client := &activeContextCaptureClient{seen: make(chan struct{}, 1)}
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{SkillsEnabled: false}, logx.Discard())
	t.Cleanup(func() { mgr.Stop() })
	if _, _, err := mgr.Create("session-1"); err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime("session-1")
	rt.mu.Lock()
	rt.messages = []llm.Message{{Role: "user", Content: "old secret"}, {Role: "assistant", Content: "old answer"}}
	rt.mu.Unlock()
	leaseCtx, release, ok, err := rt.executionGate.acquireMaintenanceContext(context.Background())
	if err != nil || !ok {
		t.Fatalf("lease: %v", err)
	}
	boundary, err := mgr.CaptureActiveContextBoundary(leaseCtx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ResetActiveContext(leaseCtx, "session-1", boundary, "dream-1"); err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := mgr.EnqueueMessage(context.Background(), "session-1", "message", "new message", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("model was not called")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 1 {
		t.Fatalf("requests=%d", len(client.requests))
	}
	for _, msg := range client.requests[0].Messages {
		if strings.Contains(msg.Content, "old secret") || strings.Contains(msg.Content, "old answer") {
			t.Fatalf("old context leaked into model request: %#v", client.requests[0].Messages)
		}
	}
}

func TestResetActiveContextRejectsChangedPrefixAndResetBoundaryConflict(t *testing.T) {
	mgr := NewManager("agent-1", nil, &llm.MockClient{}, nil, nil, nil, TurnOptions{}, nil)
	t.Cleanup(func() { mgr.Stop() })
	if _, _, err := mgr.Create("session-1"); err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime("session-1")
	rt.mu.Lock()
	rt.messages = []llm.Message{{Role: "user", Content: "old"}}
	rt.mu.Unlock()
	leaseCtx, release, ok, err := rt.executionGate.acquireMaintenanceContext(context.Background())
	if err != nil || !ok {
		t.Fatalf("lease: %v", err)
	}
	defer release()
	boundary, err := mgr.CaptureActiveContextBoundary(leaseCtx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	rt.messages[0].Content = "tampered"
	rt.mu.Unlock()
	if _, err := mgr.ResetActiveContext(leaseCtx, "session-1", boundary, "reset-1"); err == nil {
		t.Fatal("accepted boundary after prefix changed")
	}
	rt.mu.Lock()
	rt.messages[0].Content = "old"
	rt.mu.Unlock()
	if _, err := mgr.ResetActiveContext(leaseCtx, "session-1", boundary, "reset-1"); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	rt.messages = append(rt.messages, llm.Message{Role: "user", Content: "later"})
	rt.mu.Unlock()
	second, err := mgr.CaptureActiveContextBoundary(leaseCtx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ResetActiveContext(leaseCtx, "session-1", second, "reset-1"); err == nil {
		t.Fatal("accepted same reset id with a different boundary")
	}
}
